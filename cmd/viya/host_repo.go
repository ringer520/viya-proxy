package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
)

// sqlite has no row mapping, so the SELECT list and the Scan call must agree
// exactly. Both are derived from these two declarations instead of being
// hand-written at every call site: a new column added to one copy used to mean
// three other copies silently dropped it.
const hostReadColumns = `id,name,domain,listener_id,upstream,enabled,version,tls_mode,path,icon_color,dns_provider_id,custom_certificate_id,custom_headers,basic_auth_user,basic_auth_hash,ip_allowlist,ip_denylist,rate_limit_requests,rate_limit_window,type,root_path,redirect_target,redirect_status,redirect_keep_path,security_headers,max_body_bytes,allowed_methods,hide_server,strip_prefix,rewrite_to,streaming,dial_timeout,response_header_timeout,maintenance,maintenance_message,spa_fallback,browse,webdav,webdav_write`

// hostWriteColumns is the writable subset, in INSERT order. UPDATE reuses the
// same order so hostWriteValues can be passed to both statements.
var hostWriteColumns = []string{
	"name", "domain", "listener_id", "upstream", "enabled", "tls_mode", "path",
	"icon_color", "dns_provider_id", "custom_certificate_id", "custom_headers",
	"basic_auth_user", "basic_auth_hash", "ip_allowlist", "ip_denylist",
	"rate_limit_requests", "rate_limit_window", "type", "root_path",
	"redirect_target", "redirect_status", "redirect_keep_path",
	"security_headers", "max_body_bytes", "allowed_methods", "hide_server",
	"strip_prefix", "rewrite_to", "streaming", "dial_timeout",
	"response_header_timeout", "maintenance", "maintenance_message",
	"spa_fallback", "browse", "webdav", "webdav_write",
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanHost(row rowScanner) (*Host, error) {
	var h Host
	var enabled, redirectKeepPath int
	var providerID, certID sql.NullInt64
	var headerJSON, allowJSON, denyJSON, securityJSON, methodsJSON string
	var maxBody int64
	var hideServer, stripPrefix, streaming, maintenance, spaFallback, browse, webdav, webdavWrite int
	if err := row.Scan(&h.ID, &h.Name, &h.Domain, &h.ListenerID, &h.Upstream, &enabled, &h.Version, &h.TLSMode, &h.Path, &h.IconColor, &providerID, &certID, &headerJSON, &h.BasicAuthUser, &h.PasswordHash, &allowJSON, &denyJSON, &h.RateLimitRequests, &h.RateLimitWindow, &h.Type, &h.RootPath, &h.RedirectTarget, &h.RedirectStatus, &redirectKeepPath, &securityJSON, &maxBody, &methodsJSON, &hideServer, &stripPrefix, &h.RewriteTo, &streaming, &h.DialTimeout, &h.ResponseHeaderTimeout, &maintenance, &h.MaintenanceMessage, &spaFallback, &browse, &webdav, &webdavWrite); err != nil {
		return nil, err
	}
	h.Enabled = enabled == 1
	h.StripPrefix = stripPrefix == 1
	h.Streaming = streaming == 1
	h.Maintenance = maintenance == 1
	h.SpaFallback = spaFallback == 1
	h.Browse = browse == 1
	h.WebDAV = webdav == 1
	h.WebDAVWrite = webdavWrite == 1
	h.RedirectKeepPath = redirectKeepPath == 1
	h.HideServer = hideServer == 1
	h.MaxBodyBytes = maxBody
	if providerID.Valid {
		h.DNSProviderID = &providerID.Int64
	}
	if certID.Valid {
		h.CustomCertificateID = &certID.Int64
	}
	if err := json.Unmarshal([]byte(headerJSON), &h.CustomHeaders); err != nil {
		return nil, errors.New("已保存的自定义请求头无效")
	}
	if err := json.Unmarshal([]byte(securityJSON), &h.SecurityHeaders); err != nil {
		return nil, errors.New("已保存的安全响应头无效")
	}
	if err := json.Unmarshal([]byte(methodsJSON), &h.AllowedMethods); err != nil {
		return nil, errors.New("已保存的请求方法无效")
	}
	if err := json.Unmarshal([]byte(allowJSON), &h.IPAllowlist); err != nil {
		return nil, errors.New("已保存的 IP 允许列表无效")
	}
	if err := json.Unmarshal([]byte(denyJSON), &h.IPDenylist); err != nil {
		return nil, errors.New("已保存的 IP 拒绝列表无效")
	}
	h.BasicAuthEnabled = h.PasswordHash != ""
	return &h, nil
}

// hostWriteValues returns the parameters for hostWriteColumns, in the same
// order. Marshal failures are impossible for the struct types involved and
// would previously have produced an empty column, which the read path rejects;
// ignoring them keeps the behaviour identical to the old inline calls.
func hostWriteValues(h Host) []any {
	headerJSON, _ := json.Marshal(h.CustomHeaders)
	allowJSON, _ := json.Marshal(h.IPAllowlist)
	denyJSON, _ := json.Marshal(h.IPDenylist)
	securityJSON, _ := json.Marshal(h.SecurityHeaders)
	methodsJSON, _ := json.Marshal(h.AllowedMethods)
	return []any{
		h.Name, h.Domain, h.ListenerID, h.Upstream, boolInt(h.Enabled), h.TLSMode, h.Path,
		h.IconColor, h.DNSProviderID, h.CustomCertificateID, string(headerJSON),
		h.BasicAuthUser, h.PasswordHash, string(allowJSON), string(denyJSON),
		h.RateLimitRequests, h.RateLimitWindow, h.Type, h.RootPath,
		h.RedirectTarget, h.RedirectStatus, boolInt(h.RedirectKeepPath),
		string(securityJSON), h.MaxBodyBytes, string(methodsJSON), boolInt(h.HideServer),
		boolInt(h.StripPrefix), h.RewriteTo, boolInt(h.Streaming), h.DialTimeout,
		h.ResponseHeaderTimeout, boolInt(h.Maintenance), h.MaintenanceMessage,
		boolInt(h.SpaFallback), boolInt(h.Browse), boolInt(h.WebDAV), boolInt(h.WebDAVWrite),
	}
}

func hostInsertSQL() string {
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(hostWriteColumns)), ",")
	return "INSERT INTO proxy_hosts(" + strings.Join(hostWriteColumns, ",") + ") VALUES(" + placeholders + ")"
}

func hostUpdateSQL() string {
	sets := make([]string, len(hostWriteColumns))
	for i, column := range hostWriteColumns {
		sets[i] = column + "=?"
	}
	return "UPDATE proxy_hosts SET " + strings.Join(sets, ",") +
		",https_port=443,version=version+1,updated_at=CURRENT_TIMESTAMP WHERE id=? AND version=?"
}
