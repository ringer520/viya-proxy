package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// banPolicy controls the automatic "fail2ban-lite" feature that converts a
// burst of 400/401/403 responses into a temporary IP ban. It is editable via
// GET/PUT /api/v1/ban-policy and cached in memory for the hot log path.
type banPolicy struct {
	Enabled   bool     `json:"enabled"`
	Allowlist []string `json:"allowlist"`
	Threshold int      `json:"threshold"`
	WindowMin int      `json:"window_min"`
	BanMin    int      `json:"ban_min"`
}

func defaultBanPolicy() banPolicy {
	return banPolicy{Enabled: true, Threshold: 20, WindowMin: 5, BanMin: 15}
}

// allows reports whether an IP is exempt from automatic bans (exact IP or CIDR).
func (p banPolicy) allows(ip net.IP) bool {
	for _, entry := range p.Allowlist {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if stored := net.ParseIP(entry); stored != nil && stored.Equal(ip) {
			return true
		}
		if _, network, err := net.ParseCIDR(entry); err == nil && network.Contains(ip) {
			return true
		}
	}
	return false
}

func (a *App) readBanPolicy() banPolicy {
	p := defaultBanPolicy()
	var raw string
	if err := a.db.QueryRowContext(context.Background(), `SELECT value FROM panel_settings WHERE key='ban_policy'`).Scan(&raw); err == nil && raw != "" {
		var parsed banPolicy
		if json.Unmarshal([]byte(raw), &parsed) == nil {
			p = parsed
		} else {
			slog.Warn("stored ban policy is invalid, using defaults")
		}
	}
	if p.Threshold < 1 {
		p.Threshold = defaultBanPolicy().Threshold
	}
	if p.WindowMin < 1 {
		p.WindowMin = defaultBanPolicy().WindowMin
	}
	if p.BanMin < 1 {
		p.BanMin = defaultBanPolicy().BanMin
	}
	return p
}

func (a *App) loadBanPolicy() banPolicy {
	if v, ok := a.banPolicyCache.Load().(banPolicy); ok {
		return v
	}
	p := a.readBanPolicy()
	a.banPolicyCache.Store(p)
	return p
}

func (a *App) getBanPolicy(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, a.loadBanPolicy())
}

func (a *App) updateBanPolicy(w http.ResponseWriter, r *http.Request) {
	var input banPolicy
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&input) != nil {
		fail(w, http.StatusBadRequest, errors.New("封禁策略格式无效"))
		return
	}
	if input.Threshold < 1 || input.Threshold > 10000 {
		fail(w, http.StatusBadRequest, errors.New("触发阈值须在 1 到 10000 之间"))
		return
	}
	if input.WindowMin < 1 || input.WindowMin > 1440 {
		fail(w, http.StatusBadRequest, errors.New("统计窗口须在 1 到 1440 分钟之间"))
		return
	}
	if input.BanMin < 1 || input.BanMin > 43200 {
		fail(w, http.StatusBadRequest, errors.New("封禁时长须在 1 到 43200 分钟之间"))
		return
	}
	normalized := make([]string, 0, len(input.Allowlist))
	for _, entry := range input.Allowlist {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if ip := net.ParseIP(entry); ip != nil {
			normalized = append(normalized, ip.String())
			continue
		}
		if _, network, err := net.ParseCIDR(entry); err == nil {
			normalized = append(normalized, network.String())
			continue
		}
		fail(w, http.StatusBadRequest, errors.New("豁免名单包含无效 IP 或网段："+entry))
		return
	}
	input.Allowlist = normalized
	raw, _ := json.Marshal(input)
	if _, err := a.db.ExecContext(r.Context(), `INSERT INTO panel_settings(key,value) VALUES('ban_policy',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, string(raw)); err != nil {
		fail(w, http.StatusInternalServerError, errors.New("保存封禁策略失败"))
		return
	}
	a.banPolicyCache.Store(input)
	writeJSON(w, http.StatusOK, input)
}

// scheduleAutoBanApply throttles apply requests triggered by automatic bans so
// a sustained attack can't degenerate into a fork/reload storm. Manual applies
// still go straight through scheduleApply.
func (a *App) scheduleAutoBanApply() {
	now := time.Now().Unix()
	last := a.autoBanApplyAt.Load()
	if now-last < 15 {
		return
	}
	if a.autoBanApplyAt.CompareAndSwap(last, now) {
		a.scheduleApply()
	}
}

func (a *App) watchBanExpiry() {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	pending := false
	for {
		select {
		case <-a.ctx.Done():
			return
		case <-ticker.C:
			// expires_at = 0 marks a permanent ban and must never be reaped.
			result, err := a.db.ExecContext(a.ctx, `DELETE FROM ip_bans WHERE expires_at>0 AND expires_at<=?`, time.Now().Unix())
			if err == nil {
				n, _ := result.RowsAffected()
				pending = pending || n > 0
			}
			if pending {
				ctx, cancel := context.WithTimeout(a.ctx, 20*time.Second)
				err = a.apply(ctx)
				cancel()
				if err == nil {
					pending = false
				}
			}
		}
	}
}

func validateBan(input *IPBan) error {
	input.Domain = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(input.Domain), "."))
	if !domainRE.MatchString(input.Domain) {
		return errors.New("请选择有效的站点域名")
	}
	if input.IP != "foreign" {
		ranges, err := normalizeCIDRs([]string{input.IP})
		if err != nil || len(ranges) != 1 {
			return errors.New("请填写有效 IP 或 CIDR 网段")
		}
		input.IP = ranges[0]
	}
	input.Reason = strings.TrimSpace(input.Reason)
	if len(input.Reason) > 500 {
		return errors.New("封禁原因不能超过 500 字节")
	}
	// 0 means the rule never expires.
	if input.ExpiresAt != 0 && input.ExpiresAt <= time.Now().Unix() {
		return errors.New("到期时间必须在未来，或不填表示永久封禁")
	}
	return nil
}
func (a *App) saveIPBan(w http.ResponseWriter, r *http.Request) {
	var input IPBan
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&input) != nil {
		fail(w, 400, errors.New("封禁表单格式无效"))
		return
	}
	if err := validateBan(&input); err != nil {
		fail(w, 400, err)
		return
	}
	var exists int
	if err := a.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM proxy_hosts WHERE domain=?`, input.Domain).Scan(&exists); err != nil || exists == 0 {
		fail(w, 400, errors.New("所选站点不存在"))
		return
	}
	id := int64(0)
	if r.Method == http.MethodPut {
		var err error
		id, err = strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil || id < 1 {
			fail(w, 400, errors.New("无效封禁编号"))
			return
		}
		res, err := a.db.ExecContext(r.Context(), `UPDATE ip_bans SET domain=?,ip=?,reason=?,expires_at=?,source='manual' WHERE id=?`, input.Domain, input.IP, input.Reason, input.ExpiresAt, id)
		if err != nil {
			fail(w, 409, errors.New("相同站点和 IP 的封禁已存在"))
			return
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			fail(w, 404, errors.New("封禁记录不存在或已到期"))
			return
		}
	} else {
		res, err := a.db.ExecContext(r.Context(), `INSERT INTO ip_bans(domain,ip,reason,expires_at,source) VALUES(?,?,?,?,'manual')`, input.Domain, input.IP, input.Reason, input.ExpiresAt)
		if err != nil {
			fail(w, 409, errors.New("相同站点和 IP 的封禁已存在，请编辑已有记录"))
			return
		}
		id, _ = res.LastInsertId()
	}
	if err := a.apply(r.Context()); err != nil {
		writeJSON(w, 502, map[string]any{"id": id, "error": "封禁已保存，但 Caddy 未应用，请重新应用配置"})
		return
	}
	input.ID = id
	input.Source = "manual"
	writeJSON(w, 200, input)
}
