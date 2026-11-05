package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
)

// WebDAV settings manage the single file-service share. The "文件服务" page is
// now a whole-server file manager; this endpoint keeps the publish side (one
// shared directory exposed over WebDAV with its own account and read/write
// permission) in a small, dedicated dialog.

func (a *App) getWebDAVSettings(w http.ResponseWriter, r *http.Request) {
	var id int64
	var domain, rootPath, user, tlsMode, listenerID string
	var webdavWrite int
	err := a.db.QueryRowContext(r.Context(), `SELECT id,domain,root_path,basic_auth_user,tls_mode,listener_id,webdav_write FROM proxy_hosts WHERE type='files' ORDER BY id LIMIT 1`).
		Scan(&id, &domain, &rootPath, &user, &tlsMode, &listenerID, &webdavWrite)
	if errors.Is(err, sql.ErrNoRows) {
		writeJSON(w, 200, map[string]any{"configured": false})
		return
	}
	if err != nil {
		fail(w, 500, errors.New("读取配置失败"))
		return
	}
	writeJSON(w, 200, map[string]any{
		"configured":       true,
		"id":               id,
		"domain":           domain,
		"root_path":        rootPath,
		"basic_auth_user":  user,
		"tls_mode":         tlsMode,
		"listener_id":      listenerID,
		"webdav_write":     webdavWrite == 1,
	})
}

func (a *App) saveWebDAVSettings(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Domain           string `json:"domain"`
		RootPath         string `json:"root_path"`
		BasicAuthUser    string `json:"basic_auth_user"`
		BasicAuthPassword string `json:"basic_auth_password"`
		WebDAVWrite      bool   `json:"webdav_write"`
	}
	if err := decodeJSONBody(w, r, &body); err != nil {
		return
	}
	domain := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(body.Domain)), ".")
	rootPath := strings.TrimSpace(body.RootPath)
	if domain == "" || !domainRE.MatchString(domain) {
		fail(w, 400, errors.New("请填写有效的访问域名"))
		return
	}
	if rootPath == "" {
		fail(w, 400, errors.New("请填写共享目录"))
		return
	}
	if err := validateStaticRoot(rootPath); err != nil {
		fail(w, 400, err)
		return
	}

	// Locate an existing file-service host (there is at most one).
	var id int64
	var version int64
	var storedHash string
	lookupErr := a.db.QueryRowContext(r.Context(), `SELECT id,version,basic_auth_hash FROM proxy_hosts WHERE type='files' ORDER BY id LIMIT 1`).
		Scan(&id, &version, &storedHash)
	exists := lookupErr == nil
	if lookupErr != nil && !errors.Is(lookupErr, sql.ErrNoRows) {
		fail(w, 500, errors.New("读取配置失败"))
		return
	}

	h := Host{
		Name:            "WebDAV 共享",
		Domain:          domain,
		ListenerID:      "",
		Type:            "files",
		RootPath:        rootPath,
		Enabled:         true,
		TLSMode:         "none",
		Browse:          false,
		WebDAV:          true,
		WebDAVWrite:     body.WebDAVWrite,
		BasicAuthUser:   strings.TrimSpace(body.BasicAuthUser),
		BasicAuthEnabled: true,
		BasicAuthPassword: body.BasicAuthPassword,
	}
	if exists {
		h.ID = id
		h.Version = version
	}
	if err := validateHost(&h); err != nil {
		fail(w, 400, err)
		return
	}
	if err := a.validateHostListener(r.Context(), &h); err != nil {
		fail(w, 400, err)
		return
	}
	if err := a.validateTLSSettings(r.Context(), &h); err != nil {
		fail(w, 400, err)
		return
	}
	if err := a.prepareSiteStorage(&h); err != nil {
		fail(w, 400, err)
		return
	}

	if !exists {
		if h.BasicAuthUser == "" || h.BasicAuthPassword == "" {
			fail(w, 400, errors.New("请填写账号和密码"))
			return
		}
		if len(h.BasicAuthPassword) < 8 || len(h.BasicAuthPassword) > 72 {
			fail(w, 400, errors.New("密码长度需为 8-72 个字符"))
			return
		}
		hash, herr := hashPassword(h.BasicAuthPassword)
		if herr != nil {
			fail(w, 500, errors.New("无法处理密码"))
			return
		}
		h.PasswordHash = hash
		headerJSON, _ := json.Marshal(h.CustomHeaders)
		allowJSON, _ := json.Marshal(h.IPAllowlist)
		denyJSON, _ := json.Marshal(h.IPDenylist)
		securityJSON, _ := json.Marshal(h.SecurityHeaders)
		methodsJSON, _ := json.Marshal(h.AllowedMethods)
		if _, err := a.db.ExecContext(r.Context(), `INSERT INTO proxy_hosts(name,domain,listener_id,upstream,enabled,tls_mode,path,icon_color,dns_provider_id,custom_certificate_id,custom_headers,basic_auth_user,basic_auth_hash,ip_allowlist,ip_denylist,rate_limit_requests,rate_limit_window,type,root_path,spa_fallback,browse,redirect_target,redirect_status,redirect_keep_path,security_headers,max_body_bytes,allowed_methods,hide_server,strip_prefix,rewrite_to,streaming,dial_timeout,response_header_timeout,maintenance,maintenance_message,webdav,webdav_write) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, h.Name, h.Domain, h.ListenerID, h.Upstream, boolInt(h.Enabled), h.TLSMode, h.Path, h.IconColor, h.DNSProviderID, h.CustomCertificateID, string(headerJSON), h.BasicAuthUser, h.PasswordHash, string(allowJSON), string(denyJSON), h.RateLimitRequests, h.RateLimitWindow, h.Type, h.RootPath, boolInt(h.SpaFallback), boolInt(h.Browse), h.RedirectTarget, h.RedirectStatus, boolInt(h.RedirectKeepPath), string(securityJSON), h.MaxBodyBytes, string(methodsJSON), boolInt(h.HideServer), boolInt(h.StripPrefix), h.RewriteTo, boolInt(h.Streaming), h.DialTimeout, h.ResponseHeaderTimeout, boolInt(h.Maintenance), h.MaintenanceMessage, boolInt(h.WebDAV), boolInt(h.WebDAVWrite)); err != nil {
			status, mapped := classifyHostWriteError(err)
			fail(w, status, mapped)
			return
		}
	} else {
		if h.BasicAuthPassword != "" {
			if len(h.BasicAuthPassword) < 8 || len(h.BasicAuthPassword) > 72 {
				fail(w, 400, errors.New("密码长度需为 8-72 个字符"))
				return
			}
			hash, herr := hashPassword(h.BasicAuthPassword)
			if herr != nil {
				fail(w, 500, errors.New("无法处理密码"))
				return
			}
			storedHash = hash
		}
		headerJSON, _ := json.Marshal(h.CustomHeaders)
		allowJSON, _ := json.Marshal(h.IPAllowlist)
		denyJSON, _ := json.Marshal(h.IPDenylist)
		securityJSON, _ := json.Marshal(h.SecurityHeaders)
		methodsJSON, _ := json.Marshal(h.AllowedMethods)
		res, err := a.db.ExecContext(r.Context(), `UPDATE proxy_hosts SET name=?,domain=?,listener_id=?,upstream=?,enabled=?,tls_mode=?,https_port=443,path=?,icon_color=?,dns_provider_id=?,custom_certificate_id=?,custom_headers=?,basic_auth_user=?,basic_auth_hash=?,ip_allowlist=?,ip_denylist=?,rate_limit_requests=?,rate_limit_window=?,type=?,root_path=?,spa_fallback=?,browse=?,redirect_target=?,redirect_status=?,redirect_keep_path=?,security_headers=?,max_body_bytes=?,allowed_methods=?,hide_server=?,strip_prefix=?,rewrite_to=?,streaming=?,dial_timeout=?,response_header_timeout=?,maintenance=?,maintenance_message=?,webdav=?,webdav_write=?,version=version+1,updated_at=CURRENT_TIMESTAMP WHERE id=? AND version=?`, h.Name, h.Domain, h.ListenerID, h.Upstream, boolInt(h.Enabled), h.TLSMode, h.Path, h.IconColor, h.DNSProviderID, h.CustomCertificateID, string(headerJSON), h.BasicAuthUser, storedHash, string(allowJSON), string(denyJSON), h.RateLimitRequests, h.RateLimitWindow, h.Type, h.RootPath, boolInt(h.SpaFallback), boolInt(h.Browse), h.RedirectTarget, h.RedirectStatus, boolInt(h.RedirectKeepPath), string(securityJSON), h.MaxBodyBytes, string(methodsJSON), boolInt(h.HideServer), boolInt(h.StripPrefix), h.RewriteTo, boolInt(h.Streaming), h.DialTimeout, h.ResponseHeaderTimeout, boolInt(h.Maintenance), h.MaintenanceMessage, boolInt(h.WebDAV), boolInt(h.WebDAVWrite), id, h.Version)
		if err != nil {
			status, mapped := classifyHostWriteError(err)
			fail(w, status, mapped)
			return
		}
		changed, cerr := res.RowsAffected()
		if cerr != nil {
			fail(w, 500, cerr)
			return
		}
		if changed == 0 {
			fail(w, 409, errors.New("配置已被其他会话修改，请刷新后重试"))
			return
		}
	}

	if err := a.apply(r.Context()); err != nil {
		slog.Error("WebDAV settings apply failed; inspect redacted configuration history")
		failSavedApplication(w)
		return
	}
	writeJSON(w, 200, map[string]any{
		"configured":   true,
		"domain":       h.Domain,
		"root_path":    h.RootPath,
		"basic_auth_user": h.BasicAuthUser,
		"webdav_write": h.WebDAVWrite,
	})
}
