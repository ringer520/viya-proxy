package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"
)

// panelAccessRules restricts where the panel itself can be reached from. The
// guard runs before authentication, so a request that fails it never reaches
// the login form: a scanner on a VPS with every port open cannot probe for
// accounts or version strings, only a bare 403.
//
// The rules are intentionally two-dimensional. Host matching stops access by
// raw IP and DNS rebinding even when the port is reachable, while the address
// list keeps the port closed to everyone outside an operator's networks. Either
// dimension alone leaves a hole, so both are optional and combine with AND.
type panelAccessRules struct {
	Hosts     []string `json:"hosts"`
	IPs       []string `json:"ips"`
	HTTPSOnly bool     `json:"https_only"`
}

const (
	panelAccessKey      = "panel_access"
	panelAccessMaxHosts = 20
	panelAccessMaxIPs   = 20
)

func (rules panelAccessRules) empty() bool {
	return len(rules.Hosts) == 0 && len(rules.IPs) == 0 && !rules.HTTPSOnly
}

// loadPanelAccess returns the cached rules, reading them from the database on
// the first call. Hits happen on every panel request, so the database is only
// touched until the first Store.
func (a *App) loadPanelAccess() panelAccessRules {
	if cached, ok := a.panelAccessCache.Load().(panelAccessRules); ok {
		return cached
	}
	rules := a.readPanelAccess()
	a.panelAccessCache.Store(rules)
	return rules
}

func (a *App) readPanelAccess() panelAccessRules {
	var raw string
	if err := a.db.QueryRowContext(context.Background(), `SELECT value FROM panel_settings WHERE key=?`, panelAccessKey).Scan(&raw); err != nil || raw == "" {
		return panelAccessRules{}
	}
	var stored panelAccessRules
	if json.Unmarshal([]byte(raw), &stored) != nil {
		// Fail open: a corrupt row must never lock the operator out of the panel.
		slog.Warn("stored panel access rules are unreadable; the panel stays reachable")
		return panelAccessRules{}
	}
	rules, err := validatePanelAccess(stored)
	if err != nil {
		slog.Warn("stored panel access rules are invalid; the panel stays reachable", "error", err)
		return panelAccessRules{}
	}
	return rules
}

// panelAccessLocalBypass reports whether the request was made on this machine.
// Two shapes qualify: a loopback peer, and a peer that is one of this host's own
// interface addresses with no proxy hop in between. The second covers an SSH
// tunnel to a panel bound to a LAN address, which is the common recovery path
// when the panel is not bound to 127.0.0.1.
//
// Both the peer and the resolved client must agree: behind Caddy every request
// arrives from 127.0.0.1, so trusting the peer alone would disable the list.
func (a *App) panelAccessLocalBypass(peer, client string) bool {
	if isLoopbackAddress(peer) && isLoopbackAddress(client) {
		return true
	}
	if peer != client {
		return false
	}
	addr, err := netip.ParseAddr(peer)
	if err != nil {
		return false
	}
	return a.isLocalAddress(addr)
}

// isLocalAddress caches the host's interface addresses for a short window: the
// IPv6 prefix can change under a running process, and a stale set would close
// the recovery path.
func (a *App) isLocalAddress(addr netip.Addr) bool {
	a.localAddrsMu.Lock()
	defer a.localAddrsMu.Unlock()
	if time.Since(a.localAddrsAt) > time.Minute {
		a.localAddrs = localInterfaceAddrs()
		a.localAddrsAt = time.Now()
	}
	return a.localAddrs[addr.Unmap()]
}

func localInterfaceAddrs() map[netip.Addr]bool {
	out := map[netip.Addr]bool{}
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return out
	}
	for _, raw := range addrs {
		ipNet, ok := raw.(*net.IPNet)
		if !ok {
			continue
		}
		if addr, ok := netip.AddrFromSlice(ipNet.IP); ok {
			out[addr.Unmap()] = true
		}
	}
	return out
}

// panelAccessGuard enforces the rules for everything the panel serves.
func (a *App) panelAccessGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rules := a.loadPanelAccess()
		// Local requests are the recovery path: an SSH tunnel or a local script
		// keeps working even when every other address is refused, so a mistaken
		// rule can always be undone.
		peer := remoteIP(r)
		client := a.accessClientIP(r)
		if a.panelAccessLocalBypass(peer, client) {
			next.ServeHTTP(w, r)
			return
		}
		if rules.empty() {
			next.ServeHTTP(w, r)
			return
		}
		if len(rules.Hosts) > 0 && !panelHostAllowed(rules.Hosts, r.Host) {
			slog.Debug("panel access refused: host not allowed", "host", r.Host, "client", client)
			fail(w, http.StatusForbidden, errors.New("该地址不在面板允许访问的域名列表内，请使用配置好的域名打开面板"))
			return
		}
		if len(rules.IPs) > 0 && !panelIPAllowed(rules.IPs, client) {
			slog.Debug("panel access refused: address not allowed", "client", client, "host", r.Host)
			fail(w, http.StatusForbidden, errors.New("当前来源地址不在面板允许的网段内"))
			return
		}
		if rules.HTTPSOnly && !a.panelRequestIsHTTPS(r) {
			// Drop any port: TLS is served by Caddy on 443, while a direct hit on
			// the panel port can never negotiate TLS.
			target := "https://" + hostname(r.Host) + r.URL.RequestURI()
			http.Redirect(w, r, target, http.StatusPermanentRedirect)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// panelRequestIsHTTPS trusts X-Forwarded-Proto only from a configured proxy,
// otherwise a direct client could claim HTTPS and skip the redirect.
func (a *App) panelRequestIsHTTPS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	peer := net.ParseIP(remoteIP(r))
	if peer == nil || !a.isTrustedProxy(peer) {
		return false
	}
	scheme := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Proto"), ",")[0])
	return strings.EqualFold(scheme, "https")
}

func (a *App) isTrustedProxy(ip net.IP) bool {
	for _, network := range a.trustedProxies {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

func isLoopbackAddress(raw string) bool {
	addr, err := netip.ParseAddr(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	return addr.Unmap().IsLoopback()
}

// panelAccessAllowsRequest replays the guard for a request, used to refuse a
// save that would immediately cut the operator off.
func (a *App) panelAccessAllowsRequest(rules panelAccessRules, r *http.Request) bool {
	peer := remoteIP(r)
	client := a.accessClientIP(r)
	if a.panelAccessLocalBypass(peer, client) {
		return true
	}
	if len(rules.Hosts) > 0 && !panelHostAllowed(rules.Hosts, r.Host) {
		return false
	}
	if len(rules.IPs) > 0 && !panelIPAllowed(rules.IPs, client) {
		return false
	}
	if rules.HTTPSOnly && !a.panelRequestIsHTTPS(r) {
		return false
	}
	return true
}

// panelHostAllowed matches the request Host (port stripped) against the list.
// A "*.example.com" entry covers subdomains but not the bare domain.
func panelHostAllowed(allowed []string, requestHost string) bool {
	host := normalizePanelHost(requestHost)
	if host == "" {
		return false
	}
	for _, pattern := range allowed {
		if pattern == host {
			return true
		}
		if suffix, ok := strings.CutPrefix(pattern, "*"); ok && strings.HasSuffix(host, suffix) && len(host) > len(suffix) {
			return true
		}
	}
	return false
}

func panelIPAllowed(allowed []string, client string) bool {
	addr, err := netip.ParseAddr(strings.TrimSpace(client))
	if err != nil {
		return false
	}
	addr = addr.Unmap()
	for _, raw := range allowed {
		prefix, err := netip.ParsePrefix(raw)
		if err != nil {
			continue
		}
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

// normalizePanelHost strips a pasted scheme, a port and the trailing root dot,
// so "https://Viya.Example.com:443." and "viya.example.com" are one entry.
func normalizePanelHost(raw string) string {
	host := strings.TrimSpace(raw)
	if i := strings.Index(host, "://"); i >= 0 {
		host = host[i+3:]
	}
	if i := strings.IndexByte(host, '/'); i >= 0 {
		host = host[:i]
	}
	if opened := strings.HasPrefix(host, "["); opened {
		if end := strings.Index(host, "]"); end > 0 {
			host = host[1:end]
		}
	} else if name, _, err := net.SplitHostPort(host); err == nil {
		host = name
	}
	return strings.ToLower(strings.TrimSuffix(host, "."))
}

// validatePanelAccess normalizes input and rejects entries that could never
// match, so a typo fails at save time instead of locking the operator out.
func validatePanelAccess(input panelAccessRules) (panelAccessRules, error) {
	rules := panelAccessRules{HTTPSOnly: input.HTTPSOnly, Hosts: []string{}, IPs: []string{}}
	if len(input.Hosts) > panelAccessMaxHosts || len(input.IPs) > panelAccessMaxIPs {
		return panelAccessRules{}, fmt.Errorf("域名与网段各最多 %d 条", panelAccessMaxHosts)
	}
	seen := map[string]bool{}
	for _, raw := range input.Hosts {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		host := normalizePanelHost(raw)
		if host == "" {
			return panelAccessRules{}, fmt.Errorf("%s 不是有效的域名", raw)
		}
		pattern := host
		base := strings.TrimPrefix(host, "*.")
		if err := validPanelHostname(base); err != nil {
			return panelAccessRules{}, fmt.Errorf("%s 不是有效的域名：%w", raw, err)
		}
		if host != base && !strings.Contains(base, ".") {
			return panelAccessRules{}, fmt.Errorf("%s 的通配符需要至少一个点，例如 *.example.com", raw)
		}
		if strings.ContainsAny(host, "/ :") {
			return panelAccessRules{}, fmt.Errorf("%s 不是有效的域名", raw)
		}
		if !seen[pattern] {
			seen[pattern] = true
			rules.Hosts = append(rules.Hosts, pattern)
		}
	}
	for _, raw := range input.IPs {
		entry := strings.TrimSpace(raw)
		if entry == "" {
			continue
		}
		if addr, err := netip.ParseAddr(entry); err == nil {
			addr = addr.Unmap()
			if addr.Is4() {
				entry = addr.String() + "/32"
			} else {
				entry = addr.String() + "/128"
			}
		}
		prefix, err := netip.ParsePrefix(entry)
		if err != nil {
			return panelAccessRules{}, fmt.Errorf("%s 不是有效的 IP 或网段，例如 203.0.113.7 或 203.0.113.0/24", raw)
		}
		entry = prefix.Masked().String()
		if !seen[entry] {
			seen[entry] = true
			rules.IPs = append(rules.IPs, entry)
		}
	}
	return rules, nil
}

func validPanelHostname(host string) error {
	if host == "" || len(host) > 253 {
		return errors.New("长度不合法")
	}
	for _, label := range strings.Split(host, ".") {
		if label == "" || len(label) > 63 {
			return errors.New("标签长度不合法")
		}
		if strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return errors.New("标签不能以横线开头或结尾")
		}
		for _, r := range label {
			if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') {
				return errors.New("只支持字母、数字、点和横线")
			}
		}
	}
	return nil
}

func (a *App) getPanelAccess(w http.ResponseWriter, r *http.Request) {
	rules := a.loadPanelAccess()
	writeJSON(w, http.StatusOK, map[string]any{
		"hosts":        rules.Hosts,
		"ips":          rules.IPs,
		"https_only":   rules.HTTPSOnly,
		"current_host": r.Host,
		"current_ip":   a.accessClientIP(r),
	})
}

func (a *App) putPanelAccess(w http.ResponseWriter, r *http.Request) {
	var input panelAccessRules
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&input); err != nil {
		fail(w, http.StatusBadRequest, errors.New("规则格式无效"))
		return
	}
	rules, err := validatePanelAccess(input)
	if err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	if !a.panelAccessAllowsRequest(rules, r) {
		fail(w, http.StatusConflict, errors.New("当前访问地址不在新规则内，保存后会立刻断开；请先在允许的域名或网段上打开面板再保存"))
		return
	}
	if rules.empty() {
		if _, err = a.db.ExecContext(r.Context(), `DELETE FROM panel_settings WHERE key=?`, panelAccessKey); err != nil {
			fail(w, http.StatusInternalServerError, errors.New("保存面板访问规则失败"))
			return
		}
	} else {
		raw, err := json.Marshal(rules)
		if err != nil {
			fail(w, http.StatusInternalServerError, errors.New("保存面板访问规则失败"))
			return
		}
		if _, err = a.db.ExecContext(r.Context(), `INSERT INTO panel_settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, panelAccessKey, string(raw)); err != nil {
			fail(w, http.StatusInternalServerError, errors.New("保存面板访问规则失败"))
			return
		}
	}
	a.panelAccessCache.Store(rules)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "hosts": rules.Hosts, "ips": rules.IPs, "https_only": rules.HTTPSOnly})
}
