package main

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

type accessConfirmation struct {
	Domain   string `json:"domain"`
	Deadline int64  `json:"deadline"`
}

// Peel forwarding hops only when the immediate peer is explicitly trusted.
// This reports the source Caddy sees without trusting arbitrary visitor headers.
func (a *App) accessClientIP(r *http.Request) string {
	current := remoteIP(r)
	trusted := func(raw string) bool {
		ip := net.ParseIP(raw)
		for _, network := range a.trustedProxies {
			if network.Contains(ip) {
				return true
			}
		}
		return false
	}
	if !trusted(current) {
		return current
	}
	hops := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
	for i := len(hops) - 1; i >= 0 && trusted(current); i-- {
		next := strings.TrimSpace(hops[i])
		if net.ParseIP(next) == nil {
			break
		}
		current = next
	}
	return current
}

func hostname(address string) string {
	if host, _, err := net.SplitHostPort(address); err == nil {
		return strings.ToLower(host)
	}
	return strings.ToLower(strings.Trim(address, "[]"))
}
func banContainsIP(ban IPBan, ip string) bool {
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return false
	}
	addr = addr.Unmap()
	if ban.IP == "foreign" {
		for _, raw := range chinaAndPrivateRanges() {
			p, e := netip.ParsePrefix(raw)
			if e == nil && p.Contains(addr) {
				return false
			}
		}
		return true
	}
	if p, e := netip.ParsePrefix(ban.IP); e == nil {
		return p.Contains(addr)
	}
	p, e := netip.ParseAddr(ban.IP)
	return e == nil && p.Unmap() == addr
}
func (a *App) panelDomain(ctx context.Context, domain string) bool {
	var upstream string
	if a.db.QueryRowContext(ctx, `SELECT upstream FROM proxy_hosts WHERE domain=? AND type='proxy'`, domain).Scan(&upstream) != nil {
		return false
	}
	u, e := url.Parse(upstream)
	if e != nil {
		return false
	}
	_, port, e := net.SplitHostPort(a.panelListen)
	if e != nil {
		port = "8181"
	}
	if u.Port() != port {
		return false
	}
	host := u.Hostname()
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	if ip.IsLoopback() {
		return true
	}
	addresses, _ := net.InterfaceAddrs()
	for _, raw := range addresses {
		if p, e := netip.ParsePrefix(raw.String()); e == nil && p.Addr().Unmap().String() == ip.String() {
			return true
		}
	}
	return false
}
func (a *App) previewAccessRule(w http.ResponseWriter, r *http.Request) {
	var input IPBan
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 16384)).Decode(&input) != nil {
		fail(w, 400, errors.New("封禁格式无效"))
		return
	}
	if err := validateBan(&input); err != nil {
		fail(w, 400, err)
		return
	}
	ip := a.accessClientIP(r)
	panel := a.panelDomain(r.Context(), input.Domain)
	writeJSON(w, 200, map[string]any{"domain": input.Domain, "client_ip": ip, "client_blocked": banContainsIP(input, ip), "panel_domain": panel, "confirmation_required": panel, "confirmation_seconds": 120, "scope": "direct-peer", "message": "按连接到 Caddy 的来源地址匹配；CDN 或代理出口也会参与地区判断。"})
}
func (a *App) pendingAccessConfirmation(ctx context.Context) *accessConfirmation {
	var v accessConfirmation
	if a.db.QueryRowContext(ctx, `SELECT domain,deadline FROM access_rule_guards WHERE id=1`).Scan(&v.Domain, &v.Deadline) != nil {
		return nil
	}
	return &v
}
func (a *App) confirmAccessRule(w http.ResponseWriter, r *http.Request) {
	pending := a.pendingAccessConfirmation(r.Context())
	if pending == nil {
		fail(w, 409, errors.New("没有待确认的访问限制"))
		return
	}
	if pending.Deadline <= time.Now().Unix() {
		fail(w, 409, errors.New("确认已超时，正在恢复原规则"))
		return
	}
	if !certificateMatchesDomain([]string{pending.Domain}, hostname(r.Host)) {
		fail(w, 400, errors.New("请从受限制的面板域名访问并确认；局域网入口可用于取消修改"))
		return
	}
	if _, err := a.db.ExecContext(r.Context(), `DELETE FROM access_rule_guards WHERE id=1`); err != nil {
		fail(w, 500, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}
func (a *App) restoreAccessRule(ctx context.Context) error {
	var id int64
	var raw string
	if err := a.db.QueryRowContext(ctx, `SELECT ban_id,before_json FROM access_rule_guards WHERE id=1`).Scan(&id, &raw); err != nil {
		return err
	}
	var before *IPBan
	if err := json.Unmarshal([]byte(raw), &before); err != nil {
		return err
	}
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `DELETE FROM ip_bans WHERE id=?`, id); err != nil {
		return err
	}
	if before != nil {
		_, err = tx.ExecContext(ctx, `INSERT INTO ip_bans(id,domain,ip,reason,expires_at,source) VALUES(?,?,?,?,?,?)`, before.ID, before.Domain, before.IP, before.Reason, before.ExpiresAt, before.Source)
		if err != nil {
			return err
		}
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	operation, _ := ctx.Value(operationContextKey{}).(operationContext)
	operation.Summary = "恢复面板访问限制"
	if operation.Actor == "" {
		operation.Actor = "system"
	}
	if err = a.apply(context.WithValue(ctx, operationContextKey{}, operation)); err != nil {
		return err
	}
	_, err = a.db.ExecContext(ctx, `DELETE FROM access_rule_guards WHERE id=1`)
	return err
}
func (a *App) cancelAccessRule(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Confirmed bool `json:"confirmed"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&input) != nil || !input.Confirmed {
		fail(w, 400, errors.New("请确认恢复原访问规则"))
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 90*time.Second)
	defer cancel()
	if err := a.restoreAccessRule(ctx); err != nil {
		fail(w, 502, errors.New("恢复访问规则失败，将继续自动重试"))
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}
func (a *App) watchAccessRules() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-a.ctx.Done():
			return
		case <-ticker.C:
			a.configMu.Lock()
			pending := a.pendingAccessConfirmation(a.ctx)
			if pending != nil && pending.Deadline <= time.Now().Unix() {
				ctx, cancel := context.WithTimeout(a.ctx, 90*time.Second)
				_ = a.restoreAccessRule(ctx)
				cancel()
			}
			a.configMu.Unlock()
		}
	}
}
func (a *App) explainAccessRules(w http.ResponseWriter, r *http.Request) {
	domain := canonicalSiteDomain(r.URL.Query().Get("domain"))
	ip := r.URL.Query().Get("ip")
	if ip == "" {
		ip = a.accessClientIP(r)
	}
	if _, err := netip.ParseAddr(ip); err != nil {
		fail(w, 400, errors.New("请输入有效 IP"))
		return
	}
	rows, err := a.db.QueryContext(r.Context(), `SELECT id,domain,ip,reason,source,expires_at FROM ip_bans WHERE domain=? AND (expires_at=0 OR expires_at>?)`, domain, time.Now().Unix())
	if err != nil {
		fail(w, 500, err)
		return
	}
	defer rows.Close()
	matches := []IPBan{}
	for rows.Next() {
		var b IPBan
		if err = rows.Scan(&b.ID, &b.Domain, &b.IP, &b.Reason, &b.Source, &b.ExpiresAt); err != nil {
			fail(w, 500, err)
			return
		}
		if banContainsIP(b, ip) {
			matches = append(matches, b)
		}
	}
	writeJSON(w, 200, map[string]any{"ip": ip, "domain": domain, "matches": matches, "scope": "ip-ban-rules", "message": "这里只检查 IP 封禁与地区规则；安全策略中的允许名单、限速和认证需另行检查。"})
}
