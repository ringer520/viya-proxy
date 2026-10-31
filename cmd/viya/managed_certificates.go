package main

import (
	"context"
	"crypto/x509"
	"database/sql"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net"
	"net/http"
	"net/mail"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type ManagedCertificate struct {
	ID              int64  `json:"id"`
	Name            string `json:"name"`
	Domain          string `json:"domain"`
	Method          string `json:"method"`
	Email           string `json:"email"`
	ProviderID      *int64 `json:"dns_provider_id,omitempty"`
	Status          string `json:"status"`
	Progress        int    `json:"progress"`
	ProgressMessage string `json:"progress_message"`
	ProgressAttempt int    `json:"progress_attempt"`
	LastError       string `json:"last_error,omitempty"`
	NotAfter        string `json:"not_after,omitempty"`
	Issuer          string `json:"issuer,omitempty"`
	CreatedAt       string `json:"created_at"`
}

func (a *App) initCertificateRequests() error {
	_, err := a.db.Exec(`CREATE TABLE IF NOT EXISTS managed_certificates(id INTEGER PRIMARY KEY,name TEXT NOT NULL,domain TEXT NOT NULL UNIQUE,method TEXT NOT NULL,email TEXT NOT NULL DEFAULT '',dns_provider_id INTEGER REFERENCES dns_providers(id),created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP)`)
	return err
}
func (a *App) loadManagedCertificates(ctx context.Context) ([]ManagedCertificate, error) {
	rows, err := a.db.QueryContext(ctx, `SELECT id,name,domain,method,email,dns_provider_id,created_at FROM managed_certificates ORDER BY id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ManagedCertificate{}
	for rows.Next() {
		var v ManagedCertificate
		if err = rows.Scan(&v.ID, &v.Name, &v.Domain, &v.Method, &v.Email, &v.ProviderID, &v.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func validateManagedCertificate(v *ManagedCertificate) error {
	v.Name = strings.TrimSpace(v.Name)
	v.Domain = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(v.Domain), "."))
	v.Email = strings.TrimSpace(v.Email)
	if v.Name == "" || len(v.Name) > 100 {
		return errors.New("证书名称须为 1 到 100 字节")
	}
	if !domainRE.MatchString(v.Domain) || net.ParseIP(v.Domain) != nil || !strings.Contains(v.Domain, ".") {
		return errors.New("请填写不含协议、端口或路径的域名")
	}
	switch v.Method {
	case "http-01", "dns-01", "internal":
	default:
		return errors.New("不支持的证书验证方式")
	}
	if v.Method != "internal" && (strings.HasSuffix(v.Domain, ".localhost") || strings.HasSuffix(v.Domain, ".local") || strings.HasSuffix(v.Domain, ".test") || strings.HasSuffix(v.Domain, ".invalid")) {
		return errors.New("内网测试域名请选择内部 CA")
	}
	if strings.HasPrefix(v.Domain, "*.") && v.Method == "http-01" {
		return errors.New("泛域名证书必须选择 DNS-01 或内部 CA")
	}
	if v.Email != "" {
		address, err := mail.ParseAddress(v.Email)
		if err != nil || address.Address != v.Email || len(v.Email) > 254 {
			return errors.New("请填写有效邮箱地址")
		}
	}
	if v.Method == "dns-01" {
		if v.ProviderID == nil || *v.ProviderID < 1 {
			return errors.New("DNS-01 需要选择 DNS 凭据")
		}
	} else {
		v.ProviderID = nil
	}
	return nil
}
func (a *App) requestCertificate(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	var v ManagedCertificate
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&v) != nil {
		fail(w, 400, errors.New("证书申请格式无效"))
		return
	}
	if err := validateManagedCertificate(&v); err != nil {
		fail(w, 400, err)
		return
	}
	if v.ProviderID != nil {
		if _, _, err := a.loadDNSProvider(r.Context(), *v.ProviderID); err != nil {
			fail(w, 400, errors.New("DNS 凭据不存在或无法读取"))
			return
		}
	}
	var conflicts int
	if err := a.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM proxy_hosts WHERE domain=? AND tls_mode NOT IN ('auto','none')`, v.Domain).Scan(&conflicts); err != nil {
		fail(w, 500, err)
		return
	}
	if conflicts > 0 {
		fail(w, 409, errors.New("该域名站点已使用其他 TLS 配置，请先将其 TLS 模式改为自动 HTTPS"))
		return
	}
	var err error
	if r.Method == http.MethodPut {
		v.ID, err = strconv.ParseInt(r.PathValue("id"), 10, 64)
		var old string
		if err != nil || v.ID < 1 || a.db.QueryRowContext(r.Context(), `SELECT domain FROM managed_certificates WHERE id=?`, v.ID).Scan(&old) != nil {
			fail(w, 404, errors.New("申请不存在"))
			return
		}
		// Domain identity is immutable; use Clone to request another domain.
		if old != v.Domain {
			fail(w, 400, errors.New("编辑不能更改证书域名，请克隆申请新域名"))
			return
		}
		_, err = a.db.ExecContext(r.Context(), `UPDATE managed_certificates SET name=?,method=?,email=?,dns_provider_id=? WHERE id=?`, v.Name, v.Method, v.Email, v.ProviderID, v.ID)
	} else {
		var res sql.Result
		res, err = a.db.ExecContext(r.Context(), `INSERT INTO managed_certificates(name,domain,method,email,dns_provider_id) VALUES(?,?,?,?,?)`, v.Name, v.Domain, v.Method, v.Email, v.ProviderID)
		if err == nil {
			v.ID, _ = res.LastInsertId()
		}
	}
	if err != nil {
		fail(w, 409, errors.New("该域名已有托管申请，请查看证书列表"))
		return
	}
	a.setCertificateProgress(v.Domain, certificateProgress{Status: "pending", Percent: 8, Message: "申请已保存，等待 Caddy 开始签发"})
	if err = a.applyLocked(r.Context()); err != nil {
		a.setCertificateProgress(v.Domain, certificateProgress{Status: "config-error", Percent: 0, Message: "Caddy 尚未接受申请配置", LastError: "请重新应用配置后继续签发。"})
		writeJSON(w, 502, map[string]any{"id": v.ID, "error": "申请已保存，但 Caddy 未接受配置，请重新应用配置"})
		return
	}
	v.Status = "pending"
	v.Progress = 8
	v.ProgressMessage = "申请已提交，等待 Caddy 开始签发"
	writeJSON(w, 202, v)
}
func (a *App) managedPolicies(ctx context.Context) ([]any, []string, bool, error) {
	certs, err := a.loadManagedCertificates(ctx)
	if err != nil {
		return nil, nil, false, err
	}
	policies := []any{}
	domains := []string{}
	internal := false
	for _, v := range certs {
		issuer := map[string]any{"module": "internal"}
		if v.Method == "internal" {
			internal = true
		} else {
			issuer = map[string]any{"module": "acme", "ca": "https://acme-v02.api.letsencrypt.org/directory", "email": v.Email}
			if v.Method == "http-01" {
				issuer["challenges"] = map[string]any{"tls-alpn": map[string]any{"disabled": true}}
			} else {
				if v.ProviderID == nil {
					return nil, nil, false, errors.New("managed certificate DNS credentials missing")
				}
				kind, credentials, err := a.loadDNSProvider(ctx, *v.ProviderID)
				if err != nil {
					return nil, nil, false, err
				}
				provider := map[string]any{"name": kind}
				for k, val := range credentials {
					provider[k] = val
				}
				issuer["challenges"] = map[string]any{"dns": map[string]any{"provider": provider}}
			}
		}
		domains = append(domains, v.Domain)
		policies = append(policies, map[string]any{"subjects": []string{v.Domain}, "issuers": []any{issuer}})
	}
	return policies, domains, internal, nil
}
func (a *App) listManagedCertificates(w http.ResponseWriter, r *http.Request) {
	out, err := a.loadManagedCertificates(r.Context())
	if err != nil {
		fail(w, 500, err)
		return
	}
	// Read public certificate metadata only. Private keys never leave Caddy storage.
	root := filepath.Join(env("XDG_DATA_HOME", filepath.Join(env("HOME", a.dataDir), ".local", "share")), "caddy", "certificates")
	for i := range out {
		progress := a.getCertificateProgress(out[i].Domain)
		out[i].Status = progress.Status
		out[i].Progress = progress.Percent
		out[i].ProgressMessage = progress.Message
		out[i].ProgressAttempt = progress.Attempt
		out[i].LastError = progress.LastError
	}
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, e error) error {
		if e != nil || d.IsDir() || !strings.HasSuffix(path, ".crt") {
			return nil
		}
		data, e := os.ReadFile(path)
		if e != nil {
			return nil
		}
		block, _ := pem.Decode(data)
		if block == nil {
			return nil
		}
		cert, e := x509.ParseCertificate(block.Bytes)
		if e != nil {
			return nil
		}
		relative, _ := filepath.Rel(root, path)
		local := strings.HasPrefix(relative, "local"+string(os.PathSeparator))
		for i := range out {
			if (out[i].Method == "internal") != local {
				continue
			}
			for _, name := range cert.DNSNames {
				if strings.EqualFold(name, out[i].Domain) {
					if out[i].NotAfter != "" {
						prev, _ := time.Parse(time.RFC3339, out[i].NotAfter)
						if !cert.NotAfter.After(prev) {
							continue
						}
					}
					out[i].NotAfter = cert.NotAfter.UTC().Format(time.RFC3339)
					out[i].Issuer = cert.Issuer.CommonName
					out[i].Status = "issued"
					out[i].Progress = 100
					out[i].ProgressMessage = "签发成功，Caddy 正在自动续期"
					out[i].LastError = ""
					a.setCertificateProgress(out[i].Domain, certificateProgress{Status: "issued", Percent: 100, Message: out[i].ProgressMessage})
					if time.Now().After(cert.NotAfter) {
						out[i].Status = "expired"
						out[i].ProgressMessage = "证书已过期"
					} else if time.Now().Before(cert.NotBefore) {
						out[i].Status = "pending"
					}
				}
			}
		}
		return nil
	})
	writeJSON(w, 200, out)
}
func (a *App) deleteManagedCertificate(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		fail(w, 400, errors.New("无效证书编号"))
		return
	}
	var domain string
	if err = a.db.QueryRowContext(r.Context(), `SELECT domain FROM managed_certificates WHERE id=?`, id).Scan(&domain); err != nil {
		fail(w, 404, errors.New("申请不存在"))
		return
	}
	var used int
	if err = a.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM proxy_hosts WHERE domain=? AND tls_mode='auto'`, domain).Scan(&used); err != nil {
		fail(w, 500, err)
		return
	}
	if used > 0 {
		fail(w, 409, errors.New("自动 HTTPS 站点正在使用该托管配置，请先修改或移除站点"))
		return
	}
	if _, err = a.db.ExecContext(r.Context(), `DELETE FROM managed_certificates WHERE id=?`, id); err != nil {
		fail(w, 500, err)
		return
	}
	if err = a.applyLocked(r.Context()); err != nil {
		fail(w, 502, errors.New("托管配置已删除，但 Caddy 尚未重新应用"))
		return
	}
	w.WriteHeader(204)
}
