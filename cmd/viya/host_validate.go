package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"
)

func validateHost(h *Host) error {
	h.Name = strings.TrimSpace(h.Name)
	if len([]rune(h.Name)) > 100 {
		return errors.New("站点名称不能超过 100 个字符")
	}
	h.Domain = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(h.Domain)), ".")
	if h.Domain == "" || !domainRE.MatchString(h.Domain) {
		return errors.New("域名必须是有效主机名")
	}
	switch h.Type {
	case "", "proxy":
		h.Type = "proxy"
		u, err := parseUpstream(h.Upstream)
		if err != nil {
			return err
		}
		h.Upstream = u.String()
	case "static":
		h.Upstream = ""
		h.RedirectTarget = ""
		h.RedirectStatus = 0
		h.WebDAV = false
		h.WebDAVWrite = false
		// An empty root is fine here: prepareSiteStorage assigns a managed
		// directory below. Anything non-empty must be a clean absolute path.
		if strings.TrimSpace(h.RootPath) != "" {
			if err := validateStaticRoot(h.RootPath); err != nil {
				return err
			}
		}
	case "files":
		// A file service serves one shared directory over the web and/or WebDAV.
		// It never proxies, never redirects and has no SPA entry point.
		h.Upstream = ""
		h.RedirectTarget = ""
		h.RedirectStatus = 0
		h.SpaFallback = false
		if !h.Browse && !h.WebDAV {
			return errors.New("请至少开启“网页目录浏览”或“WebDAV”中的一项")
		}
		// Write access is meaningless without WebDAV; drop it instead of storing
		// a switch that cannot take effect.
		if !h.WebDAV {
			h.WebDAVWrite = false
		}
		if strings.TrimSpace(h.RootPath) != "" {
			if err := validateStaticRoot(h.RootPath); err != nil {
				return err
			}
		}
	case "redirect":
		h.Upstream = ""
		h.RootPath = ""
		target := strings.TrimSpace(h.RedirectTarget)
		if target == "" {
			return errors.New("请填写跳转目标")
		}
		u, err := url.Parse(target)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return errors.New("跳转目标必须是 http(s):// 地址")
		}
		if u.RawQuery != "" || u.Fragment != "" {
			return errors.New("跳转目标不能包含查询串或片段，请求自带的会保留")
		}
		h.RedirectTarget = strings.TrimRight(target, "/")
		switch h.RedirectStatus {
		case 0:
			h.RedirectStatus = 301
		case 301, 302, 307, 308:
		default:
			return errors.New("跳转状态码只能是 301、302、307 或 308")
		}
	default:
		return errors.New("站点类型只能是反向代理、静态站点、文件服务或跳转")
	}
	h.Path = strings.TrimSpace(h.Path)
	if h.Path != "" {
		if !strings.HasPrefix(h.Path, "/") || len(h.Path) > 256 || strings.ContainsAny(h.Path, "?#*\\\r\n\x00\t ") {
			return errors.New("访问路径须以 / 开始，不能包含空格、通配符、查询参数或片段")
		}
		h.Path = strings.TrimRight(h.Path, "/")
	}
	if h.Path == "/" {
		h.Path = ""
	}
	// Path rewriting. Stripping only makes sense against a matched prefix, and a
	// rewrite target must be an absolute path so Caddy can apply it verbatim.
	if h.StripPrefix && h.Path == "" {
		return errors.New("剥离访问路径前缀需要先填写访问路径")
	}
	h.RewriteTo = strings.TrimSpace(h.RewriteTo)
	if h.RewriteTo != "" {
		if !strings.HasPrefix(h.RewriteTo, "/") || len(h.RewriteTo) > 512 || strings.ContainsAny(h.RewriteTo, "?#\\\r\n\x00\t ") {
			return errors.New("重写路径须以 / 开始，不能包含空格、查询参数、片段或反斜杠")
		}
	}
	// Upstream tuning. Empty means "keep the Caddy default".
	h.DialTimeout = strings.TrimSpace(h.DialTimeout)
	if h.DialTimeout != "" {
		d, err := time.ParseDuration(h.DialTimeout)
		if err != nil || d <= 0 {
			return errors.New("连接超时须为合法的正时长，例如 10s、500ms")
		}
	}
	h.ResponseHeaderTimeout = strings.TrimSpace(h.ResponseHeaderTimeout)
	if h.ResponseHeaderTimeout != "" {
		d, err := time.ParseDuration(h.ResponseHeaderTimeout)
		if err != nil || d <= 0 {
			return errors.New("响应头超时须为合法的正时长，例如 30s、2m")
		}
	}
	if len([]rune(h.MaintenanceMessage)) > 500 {
		return errors.New("维护提示不能超过 500 个字符")
	}
	h.MaintenanceMessage = strings.TrimSpace(h.MaintenanceMessage)
	switch h.IconColor {
	case "", "blue":
		h.IconColor = "blue"
	case "teal", "green", "lime", "amber", "orange", "rose", "pink", "violet", "slate":
	default:
		return errors.New("无效的站点图标颜色")
	}
	if h.TLSMode == "" {
		h.TLSMode = "auto"
	}
	if h.CustomHeaders == nil {
		h.CustomHeaders = map[string]string{}
	}
	return nil
}

func parseUpstream(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return nil, errors.New("上游地址必须是 HTTP(S) 地址，例如 https://example.com")
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("上游地址不能包含用户名、密码、查询参数或片段")
	}
	if strings.HasSuffix(u.Host, ":") {
		return nil, errors.New("上游地址的端口不能为空，请删除末尾冒号或填写端口")
	}
	if strings.Contains(u.Hostname(), ":") && !strings.HasPrefix(u.Host, "[") {
		return nil, errors.New("IPv6 上游地址必须使用方括号，例如 http://[::1]")
	}
	if u.Port() == "" {
		port := "80"
		if u.Scheme == "https" {
			port = "443"
		}
		u.Host = net.JoinHostPort(u.Hostname(), port)
	}
	for _, digit := range u.Port() {
		if digit < '0' || digit > '9' {
			return nil, errors.New("上游端口必须是数字")
		}
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil || port < 1 || port > 65535 {
		return nil, errors.New("上游端口范围必须是 1 到 65535")
	}
	if u.Path != "" && u.Path != "/" {
		return nil, errors.New("暂不支持在上游地址中填写路径前缀")
	}
	return u, nil
}

func validateHeaders(headers map[string]string) error {
	if len(headers) > 40 {
		return errors.New("自定义请求头最多 40 个")
	}
	for key, value := range headers {
		if !headerNameRE.MatchString(key) || strings.EqualFold(key, "Host") || strings.EqualFold(key, "Content-Length") || strings.EqualFold(key, "Connection") || strings.EqualFold(key, "Transfer-Encoding") || strings.EqualFold(key, "Upgrade") {
			return fmt.Errorf("invalid or reserved custom header name %q", key)
		}
		if len(value) > 4096 || strings.ContainsAny(value, "\r\n\x00") {
			return fmt.Errorf("invalid value for custom header %q", key)
		}
	}
	return nil
}

func validateHostSecurity(h *Host) error {
	if h.BasicAuthEnabled {
		h.BasicAuthUser = strings.TrimSpace(h.BasicAuthUser)
		if h.BasicAuthUser == "" || len(h.BasicAuthUser) > 128 || strings.ContainsAny(h.BasicAuthUser, ":\r\n\x00") {
			return errors.New("Basic Auth 用户名需为 1-128 个字符，且不能包含冒号或控制字符")
		}
	} else {
		h.BasicAuthUser = ""
		h.BasicAuthPassword = ""
	}
	var err error
	h.IPAllowlist, err = normalizeCIDRs(h.IPAllowlist)
	if err != nil {
		return fmt.Errorf("invalid IP allowlist: %w", err)
	}
	h.IPDenylist, err = normalizeCIDRs(h.IPDenylist)
	if err != nil {
		return fmt.Errorf("invalid IP denylist: %w", err)
	}
	if h.RateLimitRequests < 0 || h.RateLimitRequests > 1000000 {
		return errors.New("限流需在每窗口 0-1000000 次之间")
	}
	if h.RateLimitRequests == 0 {
		h.RateLimitWindow = ""
		return nil
	}
	if h.RateLimitWindow == "" {
		h.RateLimitWindow = "1s"
	}
	duration, err := time.ParseDuration(h.RateLimitWindow)
	if err != nil || duration < 100*time.Millisecond || duration > 24*time.Hour {
		return errors.New("限流窗口需在 100ms 到 24 小时之间")
	}
	return nil
}

func normalizeCIDRs(input []string) ([]string, error) {
	if len(input) > 1000 {
		return nil, errors.New("IP 网段最多 1000 条")
	}
	result := make([]string, 0, len(input))
	seen := map[string]bool{}
	for _, raw := range input {
		value := strings.TrimSpace(raw)
		if value == "" {
			continue
		}
		if ip := net.ParseIP(value); ip != nil {
			if ip.To4() != nil {
				value += "/32"
			} else {
				value += "/128"
			}
		}
		_, network, err := net.ParseCIDR(value)
		if err != nil {
			return nil, errors.New("条目必须是 IP 地址或 CIDR 网段")
		}
		normalized := network.String()
		if !seen[normalized] {
			seen[normalized] = true
			result = append(result, normalized)
		}
	}
	return result, nil
}

func (a *App) validateTLSSettings(ctx context.Context, h *Host) error {
	var managed int
	if err := a.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM managed_certificates WHERE domain=?`, h.Domain).Scan(&managed); err != nil {
		return err
	}
	if managed > 0 && h.TLSMode != "auto" && h.TLSMode != "none" {
		return errors.New("该域名已配置托管证书，请选择自动 HTTPS 以使用并自动续期")
	}
	if strings.HasPrefix(h.Domain, "*.") && ((h.TLSMode == "auto" && managed == 0) || h.TLSMode == "http-01") {
		return errors.New("泛域名不能使用自动 HTTPS 或 HTTP-01，请选择 DNS-01、内部 CA 或匹配的自定义证书")
	}
	switch h.TLSMode {
	case "auto", "http-01", "none", "internal":
		if h.DNSProviderID != nil {
			return errors.New("只有在 DNS-01 模式下才能选择 DNS 凭据")
		}
	case "dns-01":
		if h.DNSProviderID == nil || *h.DNSProviderID < 1 {
			return errors.New("DNS-01 模式必须选择 DNS 凭据")
		}
		var exists int
		if err := a.db.QueryRowContext(ctx, `SELECT 1 FROM dns_providers WHERE id=?`, *h.DNSProviderID).Scan(&exists); err != nil {
			return errors.New("所选 DNS 凭据不存在")
		}
	case "custom":
		if h.DNSProviderID != nil {
			return errors.New("自定义证书不能选择 DNS 凭据")
		}
		if h.CustomCertificateID == nil || *h.CustomCertificateID < 1 {
			return errors.New("请选择自定义证书")
		}
		var namesJSON string
		if err := a.db.QueryRowContext(ctx, `SELECT dns_names_json FROM custom_certificates WHERE id=?`, *h.CustomCertificateID).Scan(&namesJSON); err != nil {
			return errors.New("所选自定义证书不存在")
		}
		var names []string
		if json.Unmarshal([]byte(namesJSON), &names) != nil || !certificateMatchesDomain(names, h.Domain) {
			return errors.New("自定义证书的域名不包含该站点")
		}
	default:
		return errors.New("TLS 模式只能是自动、HTTP-01、DNS-01、内部签发、自定义或不启用")
	}
	return nil
}

func hostUsesInternalCA(h Host) bool {
	if h.TLSMode == "internal" {
		return true
	}
	if h.TLSMode != "auto" {
		return false
	}
	name := strings.TrimPrefix(strings.ToLower(h.Domain), "*.")
	return name == "localhost" || strings.HasSuffix(name, ".localhost") || net.ParseIP(name) != nil
}

func upstreamDial(raw string) string { u, _ := url.Parse(raw); return u.Host }
