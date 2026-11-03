package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"path/filepath"
	"strconv"
	"time"
)

func (a *App) compileConfig(ctx context.Context) ([]byte, error) {
	rows, err := a.db.QueryContext(ctx, `SELECT id,domain,listener_id,upstream,tls_mode,path,icon_color,dns_provider_id,custom_certificate_id,custom_headers,basic_auth_user,basic_auth_hash,ip_allowlist,ip_denylist,rate_limit_requests,rate_limit_window,type,root_path,redirect_target,redirect_status,redirect_keep_path,security_headers,max_body_bytes,allowed_methods,hide_server FROM proxy_hosts WHERE enabled=1 ORDER BY domain`)
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
		var hideServer int
		if err := rows.Scan(&h.ID, &h.Domain, &h.ListenerID, &h.Upstream, &h.TLSMode, &h.Path, &h.IconColor, &providerID, &certID, &headers, &h.BasicAuthUser, &h.PasswordHash, &allowJSON, &denyJSON, &h.RateLimitRequests, &h.RateLimitWindow, &h.Type, &h.RootPath, &h.RedirectTarget, &h.RedirectStatus, &redirectKeepPath, &securityJSON, &maxBody, &methodsJSON, &hideServer); err != nil {
			rows.Close()
			return nil, err
		}
		h.RedirectKeepPath = redirectKeepPath == 1
		if providerID.Valid {
			h.DNSProviderID = &providerID.Int64
		}
		if certID.Valid {
			h.CustomCertificateID = &certID.Int64
		}
		if err := json.Unmarshal([]byte(headers), &h.CustomHeaders); err != nil {
			rows.Close()
			return nil, errors.New("stored custom headers are invalid")
		}
		if err := json.Unmarshal([]byte(securityJSON), &h.SecurityHeaders); err != nil {
			rows.Close()
			return nil, errors.New("stored security headers are invalid")
		}
		h.MaxBodyBytes = maxBody
		h.HideServer = hideServer == 1
		if err := json.Unmarshal([]byte(methodsJSON), &h.AllowedMethods); err != nil {
			rows.Close()
			return nil, errors.New("stored allowed methods are invalid")
		}
		if err := json.Unmarshal([]byte(allowJSON), &h.IPAllowlist); err != nil {
			rows.Close()
			return nil, errors.New("stored IP allowlist is invalid")
		}
		if err := json.Unmarshal([]byte(denyJSON), &h.IPDenylist); err != nil {
			rows.Close()
			return nil, errors.New("stored IP denylist is invalid")
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
		switch host.Type {
		case "static":
			// The configured root is stored in request variables for file_server.
			rootMatcher := map[string]any{"host": []string{host.Domain}}
			if host.Path != "" {
				rootMatcher["path"] = []string{host.Path, host.Path + "/*"}
			}
			hostRoutes = append(hostRoutes, map[string]any{"match": []any{rootMatcher}, "handle": []any{map[string]any{"handler": "vars", "root": host.RootPath}}})
			fileServer := map[string]any{"handler": "file_server", "root": host.RootPath}
			handlers = append(handlers, fileServer)
		case "redirect":
			location := host.RedirectTarget
			if host.RedirectKeepPath {
				location += "{http.request.uri}"
			}
			handlers = append(handlers, map[string]any{"handler": "static_response", "status_code": host.RedirectStatus, "headers": map[string]any{"Location": []string{location}}})
		default:
			proxy := map[string]any{"handler": "reverse_proxy", "upstreams": []any{map[string]any{"dial": upstreamDial(host.Upstream)}}}
			if upstream, parseErr := url.Parse(host.Upstream); parseErr == nil && upstream.Scheme == "https" {
				proxy["transport"] = map[string]any{"protocol": "http", "tls": map[string]any{"server_name": upstream.Hostname()}}
			}
			handlers = append(handlers, proxy)
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
				return nil, errors.New("DNS provider is missing for a DNS-01 host")
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
			return nil, errors.New("custom certificate files are unavailable")
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
