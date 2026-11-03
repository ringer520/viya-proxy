package main

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/libdns/libdns"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type providerReference struct {
	Kind string `json:"kind"`
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

func (a *App) providerReferences(ctx context.Context, id int64) ([]providerReference, error) {
	out := []providerReference{}
	for _, item := range []struct{ Kind, Query string }{{"site", `SELECT id,domain FROM proxy_hosts WHERE dns_provider_id=?`}, {"certificate", `SELECT id,domain FROM managed_certificates WHERE dns_provider_id=?`}, {"ddns", `SELECT id,name FROM ddns_records WHERE dns_provider_id=?`}} {
		rows, err := a.db.QueryContext(ctx, item.Query, id)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			v := providerReference{Kind: item.Kind}
			if err = rows.Scan(&v.ID, &v.Name); err != nil {
				rows.Close()
				return nil, err
			}
			out = append(out, v)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}
func (a *App) dnsProviderUsage(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		fail(w, 400, errors.New("无效凭据编号"))
		return
	}
	refs, err := a.providerReferences(r.Context(), id)
	if err != nil {
		fail(w, 500, err)
		return
	}
	writeJSON(w, 200, refs)
}
func (a *App) checkDNSProvider(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Zone string `json:"zone"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2048)).Decode(&input); err != nil && err != io.EOF {
		fail(w, 400, errors.New("验证参数无效"))
		return
	}
	input.Zone = strings.TrimSuffix(strings.TrimSpace(input.Zone), ".")
	if input.Zone != "" && (len(input.Zone) > 253 || !domainRE.MatchString(input.Zone) || strings.HasPrefix(input.Zone, "*.")) {
		fail(w, 400, errors.New("请输入有效 DNS Zone"))
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		fail(w, 400, errors.New("无效凭据编号"))
		return
	}
	kind, credentials, err := a.loadDNSProvider(r.Context(), id)
	if err != nil {
		fail(w, 400, errors.New("无法解密或读取凭据，请检查主密钥与配置"))
		return
	}
	status, message := "unsupported", "凭据可解密；此服务商暂不支持只读远程验证，请通过实际任务检查权限"
	count := 0
	if kind == "cloudflare" {
		ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
		defer cancel()
		zones, httpStatus, e := cloudflareZones(ctx, credentials["api_token"])
		if e == nil {
			status = "verified-read"
			message = "Zone 只读验证通过；尚未验证 DNS 写入权限"
			count = len(zones)
		} else {
			status = "network-error"
			message = "服务商查询失败，请检查网络或稍后重试"
			if httpStatus == 401 {
				status = "auth-failed"
				message = "凭据认证失败，请替换有效密钥"
			} else if httpStatus == 403 {
				status = "permission-denied"
				message = "服务商拒绝读取，请检查密钥有效性及 Zone:Read 权限"
			} else if httpStatus > 0 {
				status = "provider-error"
				message = "服务商未返回可用结果，请稍后重试"
			}
		}
	} else if input.Zone != "" {
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		provider, e := providerFor(ctx, a.loadDNSProvider, id)
		if e == nil {
			var records []libdns.Record
			records, e = provider.GetRecords(ctx, input.Zone+".")
			count = len(records)
		}
		if e == nil {
			status = "verified-read"
			message = "Zone 只读验证通过；尚未验证 DNS 写入权限"
		} else {
			status = "provider-error"
			message = "读取 DNS 记录失败，请检查 Zone 归属、网络及密钥读取权限"
		}
	}
	now := time.Now().Unix()
	_, err = a.db.ExecContext(r.Context(), `INSERT INTO provider_checks(provider_id,status,message,checked_at) VALUES(?,?,?,?) ON CONFLICT(provider_id) DO UPDATE SET status=excluded.status,message=excluded.message,checked_at=excluded.checked_at`, id, status, message, now)
	if err != nil {
		fail(w, 500, err)
		return
	}
	writeJSON(w, 200, map[string]any{"status": status, "message": message, "checked_at": now, "zones": count, "write_permission_verified": false})
}
func (a *App) renameDNSProvider(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		fail(w, 400, errors.New("无效凭据编号"))
		return
	}
	var v struct {
		Name string `json:"name"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&v) != nil || len(v.Name) < 1 || len(v.Name) > 100 {
		fail(w, 400, errors.New("名称为 1–100 字节"))
		return
	}
	v.Name = strings.TrimSpace(v.Name)
	if v.Name == "" {
		fail(w, 400, errors.New("名称不能为空"))
		return
	}
	res, err := a.db.ExecContext(r.Context(), `UPDATE dns_providers SET name=?,updated_at=CURRENT_TIMESTAMP WHERE id=?`, v.Name, id)
	if err != nil {
		fail(w, 500, err)
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		fail(w, 404, errors.New("凭据不存在"))
		return
	}
	a.refreshWorkingCheckpoint(r.Context())
	writeJSON(w, 200, map[string]bool{"ok": true})
}
