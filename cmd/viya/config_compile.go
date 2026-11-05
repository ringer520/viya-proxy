package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// maintenancePage renders the 503 body shown while a site is paused. The
// operator's message is HTML-escaped so a typo cannot inject markup.
func maintenancePage(h Host) string {
	message := strings.TrimSpace(h.MaintenanceMessage)
	if message == "" {
		message = "服务正在维护，请稍后访问。"
	}
	return "<!doctype html><html lang=\"zh-CN\"><head><meta charset=\"utf-8\">" +
		"<meta name=\"viewport\" content=\"width=device-width,initial-scale=1\">" +
		"<title>维护中</title><style>" +
		"html,body{height:100%;margin:0}" +
		"body{display:grid;place-items:center;background:#0f172a;color:#e2e8f0;" +
		"font:15px/1.7 system-ui,-apple-system,'Segoe UI',sans-serif}" +
		"main{max-width:32rem;padding:2.5rem;text-align:center}" +
		"h1{font-size:3.25rem;margin:0 0 .5rem;letter-spacing:-.03em;color:#60a5fa}" +
		"p{margin:0;color:#94a3b8}" +
		"</style></head><body><main><h1>503</h1><p>" + html.EscapeString(message) +
		"</p></main></body></html>"
}

func (a *App) compileConfig(ctx context.Context) ([]byte, error) {
	rows, err := a.db.QueryContext(ctx, `SELECT id,domain,listener_id,upstream,tls_mode,path,icon_color,dns_provider_id,custom_certificate_id,custom_headers,basic_auth_user,basic_auth_hash,ip_allowlist,ip_denylist,rate_limit_requests,rate_limit_window,type,root_path,redirect_target,redirect_status,redirect_keep_path,security_headers,max_body_bytes,allowed_methods,hide_server,strip_prefix,rewrite_to,streaming,dial_timeout,response_header_timeout,maintenance,maintenance_message,spa_fallback,browse,webdav,webdav_write FROM proxy_hosts WHERE enabled=1 ORDER BY domain`)
	if err != nil {
		return nil, err
	}
	hosts := []Host{}
	for rows.Next() {
		var h Host
		var providerID sql.NullInt64
		var certID sql.NullInt64
		var redirectKeepPath int
		var headers, allowJSON, denyJSON, securityJSON, methodsJSON string
		var maxBody int64
		var hideServer, stripPrefix, streaming, maintenance, spaFallback, browse, webdav, webdavWrite int
		if err := rows.Scan(&h.ID, &h.Domain, &h.ListenerID, &h.Upstream, &h.TLSMode, &h.Path, &h.IconColor, &providerID, &certID, &headers, &h.BasicAuthUser, &h.PasswordHash, &allowJSON, &denyJSON, &h.RateLimitRequests, &h.RateLimitWindow, &h.Type, &h.RootPath, &h.RedirectTarget, &h.RedirectStatus, &redirectKeepPath, &securityJSON, &maxBody, &methodsJSON, &hideServer, &stripPrefix, &h.RewriteTo, &streaming, &h.DialTimeout, &h.ResponseHeaderTimeout, &maintenance, &h.MaintenanceMessage, &spaFallback, &browse, &webdav, &webdavWrite); err != nil {
			rows.Close()
			return nil, err
		}
		h.RedirectKeepPath = redirectKeepPath == 1
		h.StripPrefix = stripPrefix == 1
		h.Streaming = streaming == 1
		h.Maintenance = maintenance == 1
		h.SpaFallback = spaFallback == 1
		h.Browse = browse == 1
		h.WebDAV = webdav == 1
		h.WebDAVWrite = webdavWrite == 1
		if providerID.Valid {
			h.DNSProviderID = &providerID.Int64
		}
		if certID.Valid {
			h.CustomCertificateID = &certID.Int64
		}
		if err := json.Unmarshal([]byte(headers), &h.CustomHeaders); err != nil {
			rows.Close()
			return nil, errors.New("已保存的自定义请求头无效")
		}
		if err := json.Unmarshal([]byte(securityJSON), &h.SecurityHeaders); err != nil {
			rows.Close()
			return nil, errors.New("已保存的安全响应头无效")
		}
		h.MaxBodyBytes = maxBody
		h.HideServer = hideServer == 1
		if err := json.Unmarshal([]byte(methodsJSON), &h.AllowedMethods); err != nil {
			rows.Close()
			return nil, errors.New("已保存的请求方法无效")
		}
		if err := json.Unmarshal([]byte(allowJSON), &h.IPAllowlist); err != nil {
			rows.Close()
			return nil, errors.New("已保存的 IP 允许列表无效")
		}
		if err := json.Unmarshal([]byte(denyJSON), &h.IPDenylist); err != nil {
			rows.Close()
			return nil, errors.New("已保存的 IP 拒绝列表无效")
		}
		hosts = append(hosts, h)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	globalPolicy, domainPolicies := a.readSecurityGlobal(ctx), a.readSecurityDomains(ctx)
	for i := range hosts {
		effectiveHostPolicy(&hosts[i], globalPolicy, domainPolicies)
	}
	routesByListener := map[string][]any{}
	policies, managedDomains, managedInternal, err := a.managedPolicies(ctx)
	if err != nil {
		return nil, err
	}
	noTLSHosts := []string{}
	usesInternalTLS := managedInternal
	// Ban rules are a security policy: if they cannot be read, refuse to push a
	// configuration that silently drops every rule. Capture all errors instead
	// of continuing with an empty map.
	now := time.Now().Unix()
	if _, err := a.db.ExecContext(ctx, `DELETE FROM ip_bans WHERE expires_at>0 AND expires_at<=?`, now); err != nil {
		return nil, err
	}
	banRows, err := a.db.QueryContext(ctx, `SELECT id,domain,ip FROM ip_bans WHERE expires_at=0 OR expires_at>? ORDER BY domain,ip`, now)
	if err != nil {
		return nil, err
	}
	bansByDomain := map[string][]string{}
	banIDs := map[string]int64{}
	for banRows.Next() {
		var d, ip string
		var id int64
		if err := banRows.Scan(&id, &d, &ip); err != nil {
			banRows.Close()
			return nil, err
		}
		bansByDomain[d] = append(bansByDomain[d], ip)
		banIDs[d+"|"+ip] = id
	}
	banRows.Close()
	if err := banRows.Err(); err != nil {
		return nil, err
	}
	for _, host := range hosts {
		if host.TLSMode == "none" {
			noTLSHosts = append(noTLSHosts, host.Domain)
		}
		hostRoutes := []any{}
		hostMatcher := map[string]any{"host": []string{host.Domain}}
		if host.Path != "" {
			hostMatcher["path"] = []string{host.Path, host.Path + "/*"}
		}
		handlers := []any{}
		if len(host.IPDenylist) > 0 {
			hostRoutes = append(hostRoutes, map[string]any{"match": []any{map[string]any{"host": []string{host.Domain}, "remote_ip": map[string]any{"ranges": host.IPDenylist}}}, "handle": []any{map[string]any{"handler": "static_response", "status_code": 403, "body": "Forbidden"}}})
		}
		if active := bansByDomain[host.Domain]; len(active) > 0 {
			ranges := []string{}
			for _, ip := range active {
				if ip == "foreign" {
					hostRoutes = append(hostRoutes, map[string]any{"match": []any{map[string]any{"host": []string{host.Domain}, "not": []any{map[string]any{"remote_ip": map[string]any{"ranges": chinaAndPrivateRanges()}}}}}, "handle": []any{map[string]any{"handler": "static_response", "status_code": 403, "body": "Region access denied", "headers": map[string][]string{"X-Viya-Access-Rule": {strconv.FormatInt(banIDs[host.Domain+"|foreign"], 10)}}}}})
				} else {
					ranges = append(ranges, ip)
				}
			}
			active = ranges
			if len(active) > 0 {
				hostRoutes = append(hostRoutes, map[string]any{"match": []any{map[string]any{"host": []string{host.Domain}, "remote_ip": map[string]any{"ranges": active}}}, "handle": []any{map[string]any{"handler": "static_response", "status_code": 403, "body": "Temporarily blocked"}}})
			}
		}
		if len(host.IPAllowlist) > 0 {
			hostRoutes = append(hostRoutes, map[string]any{"match": []any{map[string]any{"host": []string{host.Domain}, "not": []any{map[string]any{"remote_ip": map[string]any{"ranges": host.IPAllowlist}}}}}, "handle": []any{map[string]any{"handler": "static_response", "status_code": 403, "body": "Forbidden"}}})
		}
		if host.PasswordHash != "" {
			handlers = append(handlers, map[string]any{"handler": "authentication", "providers": map[string]any{"http_basic": map[string]any{"accounts": []any{map[string]any{"username": host.BasicAuthUser, "password": host.PasswordHash}}}}})
		}
		if host.RateLimitRequests > 0 {
			zoneName := fmt.Sprintf("proxy_host_%d", host.ID)
			handlers = append(handlers, map[string]any{"handler": "rate_limit", "rate_limits": map[string]any{zoneName: map[string]any{"key": "{http.request.remote.host}", "window": host.RateLimitWindow, "max_events": host.RateLimitRequests}}})
		}
		if len(host.CustomHeaders) > 0 {
			setHeaders := map[string][]string{}
			for key, value := range host.CustomHeaders {
				setHeaders[key] = []string{value}
			}
			handlers = append(handlers, map[string]any{"handler": "headers", "request": map[string]any{"set": setHeaders}})
		}
		responseHeaders := map[string]any{}
		if respHeaders := renderSecurityHeaders(host.SecurityHeaders); len(respHeaders) > 0 {
			responseHeaders["set"] = respHeaders
		}
		if host.HideServer {
			responseHeaders["delete"] = []string{"Server", "X-Powered-By"}
		}
		if len(responseHeaders) > 0 {
			handlers = append(handlers, map[string]any{"handler": "headers", "response": responseHeaders})
		}
		if host.MaxBodyBytes > 0 {
			handlers = append(handlers, map[string]any{"handler": "request_body", "max_size": host.MaxBodyBytes})
		}
		// Response compression (gzip + zstd) for proxied and static content.
		// Redirects return an empty body, so encode is skipped for them.
		if host.Type != "redirect" {
			handlers = append(handlers, map[string]any{
				"handler":   "encode",
				"encodings": map[string]any{"gzip": map[string]any{}, "zstd": map[string]any{}},
			})
		}
		if len(host.AllowedMethods) > 0 {
			methodMatcher := map[string]any{
				"host": []string{host.Domain},
				"not":  []any{map[string]any{"method": host.AllowedMethods}},
			}
			if host.Path != "" {
				methodMatcher["path"] = []string{host.Path, host.Path + "/*"}
			}
			hostRoutes = append(hostRoutes, map[string]any{
				"match":  []any{methodMatcher},
				"handle": []any{map[string]any{"handler": "static_response", "status_code": 405, "body": "Method Not Allowed"}},
			})
		}
		// Path rewriting runs before the terminal handler; both options apply to
		// every site type. Maintenance mode skips them because it never forwards.
		// Caddy has no standalone "uri" handler: the Caddyfile `uri strip_prefix`
		// directive compiles to a rewrite handler using strip_path_prefix.
		if !host.Maintenance {
			if host.StripPrefix && host.Path != "" {
				handlers = append(handlers, map[string]any{"handler": "rewrite", "strip_path_prefix": host.Path})
			}
			if host.RewriteTo != "" {
				handlers = append(handlers, map[string]any{"handler": "rewrite", "uri": host.RewriteTo})
			}
		}
		if host.Maintenance {
			// The security middleware above (auth, rate limit, headers) stays in
			// effect; only the terminal handler is replaced by a 503 page.
			handlers = append(handlers, map[string]any{
				"handler":     "static_response",
				"status_code": 503,
				"body":        maintenancePage(host),
				"headers": map[string][]string{
					"Content-Type":  {"text/html; charset=utf-8"},
					"Retry-After":   {"3600"},
					"Cache-Control": {"no-store"},
				},
			})
		} else {
			// Snapshot the middleware chain built so far. Site types that need
			// more than one terminal route (file services) reuse it so auth,
			// headers and rate limits apply to every entry point equally.
			securityChain := append([]any{}, handlers...)
			switch host.Type {
			case "static":
				// The configured root is stored in request variables for file_server.
				rootMatcher := map[string]any{"host": []string{host.Domain}}
				if host.Path != "" {
					rootMatcher["path"] = []string{host.Path, host.Path + "/*"}
				}
				hostRoutes = append(hostRoutes, map[string]any{"match": []any{rootMatcher}, "handle": []any{map[string]any{"handler": "vars", "root": host.RootPath}}})
				fileServer := map[string]any{"handler": "file_server", "root": host.RootPath}
				if host.Browse {
					// Empty object = default directory listing template.
					fileServer["browse"] = map[string]any{}
				}
				handlers = append(handlers, fileServer)
			case "files":
				// One shared directory, up to two entry points. WebDAV verbs are
				// matched on the method so they never reach file_server, and the
				// browser keeps a normal directory listing.
				pathMatch := []string(nil)
				if host.Path != "" {
					pathMatch = []string{host.Path, host.Path + "/*"}
				}
				writeMethods := []string{"PUT", "DELETE", "MKCOL", "COPY", "MOVE", "PROPPATCH", "LOCK", "UNLOCK"}
				if host.WebDAV {
					readMethods := []string{"PROPFIND", "OPTIONS"}
					davMethods := readMethods
					if host.WebDAVWrite {
						davMethods = append(append([]string{}, readMethods...), writeMethods...)
					} else {
						// Reject write verbs explicitly; a bare 405 from file_server
						// would not tell the client that write access is disabled.
						blockMatcher := map[string]any{"host": []string{host.Domain}, "method": writeMethods}
						if pathMatch != nil {
							blockMatcher["path"] = pathMatch
						}
						hostRoutes = append(hostRoutes, map[string]any{
							"match": []any{blockMatcher},
							"handle": append(append([]any{}, securityChain...), map[string]any{
								"handler":     "static_response",
								"status_code": 405,
								"body":        "WebDAV write access is disabled for this share.",
								"headers":     map[string][]string{"Content-Type": {"text/plain; charset=utf-8"}, "Allow": {"PROPFIND, OPTIONS, GET, HEAD"}},
							}),
						})
					}
					davMatcher := map[string]any{"host": []string{host.Domain}, "method": davMethods}
					if pathMatch != nil {
						davMatcher["path"] = pathMatch
					}
					hostRoutes = append(hostRoutes, map[string]any{
						"match": []any{davMatcher},
						"handle": append(append([]any{}, securityChain...), map[string]any{
							"handler": "webdav",
							"root":    host.RootPath,
						}),
					})
				}
				fileServer := map[string]any{"handler": "file_server", "root": host.RootPath}
				if host.Browse {
					fileServer["browse"] = map[string]any{}
				}
				handlers = append(handlers, fileServer)
			case "redirect":
				location := host.RedirectTarget
				if host.RedirectKeepPath {
					location += "{http.request.uri}"
				}
				handlers = append(handlers, map[string]any{"handler": "static_response", "status_code": host.RedirectStatus, "headers": map[string]any{"Location": []string{location}}})
			default:
				proxy := map[string]any{"handler": "reverse_proxy", "upstreams": []any{map[string]any{"dial": upstreamDial(host.Upstream)}}}
				// -1 disables response buffering so SSE and token-by-token output
				// reach the browser as soon as the upstream writes it.
				if host.Streaming {
					proxy["flush_interval"] = -1
				}
				transport := map[string]any{}
				usesTLS := false
				if upstream, parseErr := url.Parse(host.Upstream); parseErr == nil && upstream.Scheme == "https" {
					usesTLS = true
					transport["tls"] = map[string]any{"server_name": upstream.Hostname()}
				}
				// Caddy decodes the transport by its "protocol" module name, so
				// any non-empty transport must carry it, not just the TLS case.
				if usesTLS || host.DialTimeout != "" || host.ResponseHeaderTimeout != "" {
					transport["protocol"] = "http"
				}
				if host.DialTimeout != "" {
					transport["dial_timeout"] = host.DialTimeout
				}
				if host.ResponseHeaderTimeout != "" {
					transport["response_header_timeout"] = host.ResponseHeaderTimeout
				}
				if len(transport) > 0 {
					proxy["transport"] = transport
				}
				handlers = append(handlers, proxy)
			}
		}
		hostRoutes = append(hostRoutes, map[string]any{"match": []any{hostMatcher}, "handle": handlers})
		routesByListener[host.ListenerID] = append(routesByListener[host.ListenerID], hostRoutes...)
		if host.TLSMode == "http-01" {
			policies = append(policies, map[string]any{"subjects": []string{host.Domain}, "issuers": []any{map[string]any{"module": "acme", "challenges": map[string]any{"http": map[string]any{}}}}})
		}
		if hostUsesInternalCA(host) {
			usesInternalTLS = true
		}
		if host.TLSMode == "internal" {
			policies = append(policies, map[string]any{"subjects": []string{host.Domain}, "issuers": []any{map[string]any{"module": "internal"}}})
		}
		if host.TLSMode == "dns-01" {
			if host.DNSProviderID == nil {
				return nil, errors.New("DNS-01 站点缺少 DNS 凭据")
			}
			providerType, credentials, err := a.loadDNSProvider(ctx, *host.DNSProviderID)
			if err != nil {
				return nil, err
			}
			provider := map[string]any{"name": providerType}
			for key, value := range credentials {
				provider[key] = value
			}
			policies = append(policies, map[string]any{"subjects": []string{host.Domain}, "issuers": []any{map[string]any{"module": "acme", "challenges": map[string]any{"dns": map[string]any{"provider": provider}}}}})
		}
	}
	listeners, err := a.webListeners(ctx)
	if err != nil {
		return nil, err
	}
	servers := map[string]any{}
	// Each site must redirect to the HTTPS port of the listener it is attached
	// to; using one global "primary" port sent sites on :8443 to :443, where
	// they fall through to the 404 handler.
	httpsPorts := map[string]int{}
	primaryTLS := 0
	for _, listener := range listeners {
		if listener.Protocol != "https" {
			continue
		}
		httpsPorts[listener.ID] = listener.Port
		if primaryTLS == 0 {
			primaryTLS = listener.Port
		}
	}
	for _, listener := range listeners {
		serverRoutes := append([]any{}, routesByListener[listener.ID]...)
		server := map[string]any{"listen": []string{fmt.Sprintf(":%d", listener.Port)}, "logs": map[string]any{"default_logger_name": "viya_access_" + listener.ID, "should_log_credentials": false}}
		if listener.Protocol == "https" {
			server["tls_connection_policies"] = []any{map[string]any{}}
			server["automatic_https"] = map[string]any{"skip": noTLSHosts, "disable_redirects": true}
		} else {
			redirects := []any{}
			for _, host := range hosts {
				if host.TLSMode == "none" {
					continue
				}
				port := httpsPorts[host.ListenerID]
				if port == 0 {
					port = primaryTLS
				}
				if port == 0 {
					continue
				}
				address := "{http.request.host}"
				if port != 443 {
					address = net.JoinHostPort(address, strconv.Itoa(port))
				}
				redirects = append(redirects, map[string]any{"match": []any{map[string]any{"host": []string{host.Domain}}}, "handle": []any{map[string]any{"handler": "static_response", "status_code": 308, "headers": map[string][]string{"Location": {"https://" + address + "{http.request.uri}"}}}}})
			}
			serverRoutes = append(serverRoutes, redirects...)
			server["automatic_https"] = map[string]any{"disable": true}
		}
		// Unmatched and disabled sites must not look like a healthy empty response.
		serverRoutes = append(serverRoutes, map[string]any{"handle": []any{map[string]any{"handler": "static_response", "status_code": 404, "body": "Site unavailable"}}})
		server["routes"] = serverRoutes
		servers[fmt.Sprintf("viya_%s_%d", listener.Protocol, listener.Port)] = server
	}
	apps := map[string]any{"http": map[string]any{"servers": servers}}
	if usesInternalTLS {
		// Viya runs Caddy as an unprivileged service. Internal CA roots must be
		// distributed and trusted explicitly by the operator, so do not ask Caddy
		// to invoke sudo or modify the host's trust stores during config reloads.
		apps["pki"] = map[string]any{"certificate_authorities": map[string]any{"local": map[string]any{"install_trust": false}}}
	}
	l4Rows, err := a.db.QueryContext(ctx, `SELECT id,name,listen_address,listen_port,upstream,protocol FROM layer4_rules WHERE enabled=1 ORDER BY listen_port,id`)
	if err != nil {
		return nil, err
	}
	l4Servers := map[string]any{}
	for l4Rows.Next() {
		var rule Layer4Rule
		if err := l4Rows.Scan(&rule.ID, &rule.Name, &rule.ListenAddress, &rule.ListenPort, &rule.Upstream, &rule.Protocol); err != nil {
			l4Rows.Close()
			return nil, err
		}
		for _, protocol := range l4Protocols(rule.Protocol) {
			serverName := fmt.Sprintf("viya_%s_%d", protocol, rule.ID)
			listen := fmt.Sprintf("%s/%s", protocol, net.JoinHostPort(rule.ListenAddress, strconv.Itoa(rule.ListenPort)))
			l4Servers[serverName] = map[string]any{"listen": []string{listen}, "routes": []any{map[string]any{"handle": []any{map[string]any{"handler": "proxy", "upstreams": []any{map[string]any{"dial": []string{l4Dial(protocol, rule.Upstream)}}}}}}}}
		}
	}
	if err := l4Rows.Err(); err != nil {
		l4Rows.Close()
		return nil, err
	}
	l4Rows.Close()
	if len(l4Servers) > 0 {
		apps["layer4"] = map[string]any{"servers": l4Servers}
	}
	tlsApp := map[string]any{}
	policies = dedupeTLSAutomationPolicies(policies)
	if len(policies) > 0 {
		tlsApp["automation"] = map[string]any{"policies": policies}
	}
	certificates := []any{}
	seenCerts := map[int64]bool{}
	for _, host := range hosts {
		if host.TLSMode != "custom" || host.CustomCertificateID == nil || seenCerts[*host.CustomCertificateID] {
			continue
		}
		var certPath, keyPath string
		if err := a.db.QueryRowContext(ctx, `SELECT cert_path,key_path FROM custom_certificates WHERE id=?`, *host.CustomCertificateID).Scan(&certPath, &keyPath); err != nil {
			return nil, errors.New("自定义证书文件不可用")
		}
		if certPath == "" || keyPath == "" {
			return nil, fmt.Errorf("custom certificate %d has no stored file paths; remove or re-upload it", *host.CustomCertificateID)
		}
		certificates = append(certificates, map[string]any{"certificate": certPath, "key": keyPath})
		seenCerts[*host.CustomCertificateID] = true
	}
	if len(certificates) > 0 {
		tlsApp["certificates"] = map[string]any{"load_files": certificates}
	}
	if len(managedDomains) > 0 {
		loaders, ok := tlsApp["certificates"].(map[string]any)
		if !ok {
			loaders = map[string]any{}
			tlsApp["certificates"] = loaders
		}
		loaders["automate"] = managedDomains
	}
	if len(tlsApp) > 0 {
		apps["tls"] = tlsApp
	}
	config := map[string]any{"admin": map[string]any{"listen": a.adminListen, "config": map[string]any{"persist": false}}, "apps": apps}
	logConfig, logErr := a.loadLogSettings()
	if logErr != nil {
		return nil, logErr
	}
	config["logging"] = map[string]any{"logs": map[string]any{"default": map[string]any{"level": logConfig.Level}, "viya_access": map[string]any{"level": "INFO", "writer": map[string]any{"output": "file", "filename": filepath.Join(a.dataDir, "access.log"), "roll_size_mb": logConfig.AccessMB, "roll_keep": logConfig.Backups, "roll_keep_days": logConfig.Days, "roll_compression": "gzip"}, "encoder": map[string]any{"format": "json"}}, "layer4": map[string]any{"level": "DEBUG", "writer": map[string]any{"output": "file", "filename": filepath.Join(a.dataDir, "l4-access.log"), "roll_size_mb": logConfig.AccessMB, "roll_keep": logConfig.Backups, "roll_keep_days": logConfig.Days, "roll_compression": "gzip"}, "encoder": map[string]any{"format": "json"}}}}
	body, err := json.Marshal(config)
	if err != nil {
		return nil, err
	}
	return body, nil
}

// A site imported onto another HTTPS listener is still the same TLS subject.
// Caddy rejects duplicate identical automation policies for that subject, so
// keep one copy of each generated policy while retaining distinct issuers.
func dedupeTLSAutomationPolicies(policies []any) []any {
	seen := make(map[string]struct{}, len(policies))
	result := make([]any, 0, len(policies))
	for _, policy := range policies {
		encoded, err := json.Marshal(policy)
		if err != nil {
			result = append(result, policy)
			continue
		}
		key := string(encoded)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, policy)
	}
	return result
}
