package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
)

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
