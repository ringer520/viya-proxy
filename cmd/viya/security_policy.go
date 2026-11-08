package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
)

// Empty modes are legacy settings: enabled domains override, disabled ones fall through.
func resolvePolicySource(domain string, global SitePolicy, domains map[string]SitePolicy) (SitePolicy, string) {
	candidate := canonicalSiteDomain(domain)
	for strings.Contains(candidate, ".") {
		if p, ok := domains[candidate]; ok {
			if p.Mode == "none" {
				return SitePolicy{Mode: "none"}, candidate
			}
			if p.Mode == "custom" || (p.Mode == "" && p.Enabled) {
				return p, candidate
			}
		}
		candidate = candidate[strings.Index(candidate, ".")+1:]
	}
	if global.Mode == "none" || (global.Mode == "" && !global.Enabled) {
		return SitePolicy{Mode: "none"}, "global"
	}
	return global, "global"
}

func sitePolicyMode(h SecurityHeaders) string {
	if h.Mode != "" {
		return h.Mode
	}
	if h.Inherit {
		return "follow"
	}
	return "custom"
}

// Authentication and upstream headers remain site-owned regardless of policy mode.
func effectiveHostPolicy(h *Host, global SitePolicy, domains map[string]SitePolicy) {
	mode := sitePolicyMode(h.SecurityHeaders)
	if mode == "custom" {
		h.SecurityHeaders.Mode = "custom"
		h.SecurityHeaders.Inherit = false
		h.PolicySource = "custom"
		return
	}
	p := SitePolicy{}
	source := "none"
	if mode == "follow" {
		p, source = resolvePolicySource(h.Domain, global, domains)
	}
	h.SecurityHeaders = SecurityHeaders{Mode: mode, Inherit: mode == "follow", Enabled: p.Enabled, FrameOptions: p.FrameOptions, NoSniff: p.NoSniff, HSTS: p.HSTS, ReferrerPolicy: p.ReferrerPolicy, CSP: p.CSP}
	h.IPAllowlist = p.IPAllowlist
	h.IPDenylist = p.IPDenylist
	h.RateLimitRequests = p.RateLimit
	h.RateLimitWindow = p.RateLimitWin
	h.MaxBodyBytes = p.MaxBodyBytes
	h.AllowedMethods = p.AllowedMethods
	h.HideServer = p.HideServer
	h.PolicySource = source
	// API arrays stay arrays rather than null for the form editor.
	if h.IPAllowlist == nil {
		h.IPAllowlist = []string{}
	}
	if h.IPDenylist == nil {
		h.IPDenylist = []string{}
	}
	if h.AllowedMethods == nil {
		h.AllowedMethods = []string{}
	}
}

func (a *App) prepareHostPolicy(ctx context.Context, h *Host) {
	if h.SecurityHeaders.Mode == "" {
		// Compatibility for pre-mode clients; an explicit new 'none' never auto-inherits.
		a.applyPolicyInheritance(ctx, h)
	}
	effectiveHostPolicy(h, a.readSecurityGlobal(ctx), a.readSecurityDomains(ctx))
}

type securityImpact struct {
	Updated     int      `json:"updated"`
	Independent int      `json:"independent"`
	Overridden  int      `json:"overridden"`
	Sites       []string `json:"sites"`
}

func updatedPolicies(global SitePolicy, domains map[string]SitePolicy, domain string, p SitePolicy) (SitePolicy, map[string]SitePolicy) {
	next := make(map[string]SitePolicy, len(domains)+1)
	for k, v := range domains {
		next[k] = v
	}
	if domain == "" {
		global = p
	} else if p.Mode == "inherit" || (p.Mode == "" && !p.Enabled) {
		delete(next, domain)
	} else {
		next[domain] = p
	}
	return global, next
}
func (a *App) policyImpact(ctx context.Context, domain string, p SitePolicy) (securityImpact, error) {
	result := securityImpact{Sites: []string{}}
	global, domains := a.readSecurityGlobal(ctx), a.readSecurityDomains(ctx)
	nextGlobal, nextDomains := updatedPolicies(global, domains, domain, p)
	rows, err := a.db.QueryContext(ctx, `SELECT domain,security_headers FROM proxy_hosts`)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var name, raw string
		if err := rows.Scan(&name, &raw); err != nil {
			return result, err
		}
		canonical := canonicalSiteDomain(name)
		if domain != "" && canonical != domain && !domainUnder(canonical, domain) {
			continue
		}
		var headers SecurityHeaders
		if err := json.Unmarshal([]byte(raw), &headers); err != nil {
			return result, err
		}
		if sitePolicyMode(headers) != "follow" {
			result.Independent++
			continue
		}
		_, before := resolvePolicySource(name, global, domains)
		_, after := resolvePolicySource(name, nextGlobal, nextDomains)
		target := domain
		if target == "" {
			target = "global"
		}
		if before == target || after == target {
			result.Updated++
			result.Sites = append(result.Sites, name)
		} else {
			result.Overridden++
		}
	}
	return result, rows.Err()
}
func (a *App) previewSecurityPolicy(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Domain string     `json:"domain"`
		Policy SitePolicy `json:"policy"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&input); err != nil {
		fail(w, 400, errors.New("请求内容不是有效的 JSON"))
		return
	}
	if input.Policy.Mode == "none" || input.Policy.Mode == "inherit" {
		input.Policy = SitePolicy{Mode: input.Policy.Mode}
	}
	if err := validateSitePolicy(input.Policy); err != nil {
		fail(w, 400, err)
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	impact, err := a.policyImpact(r.Context(), canonicalSiteDomain(input.Domain), input.Policy)
	if err != nil {
		fail(w, 500, err)
		return
	}
	writeJSON(w, 200, impact)
}

// Keep the exact live config in memory so even an ambiguous /load timeout can be undone.
func (a *App) livePolicyConfig(ctx context.Context) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(a.adminURL, "/")+"/config/", nil)
	if err != nil {
		return nil, err
	}
	client := http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil, errors.New("无法获取 Caddy 配置")
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 20<<20))
	if err != nil {
		return nil, err
	}
	if !json.Valid(body) {
		return nil, errors.New("当前运行的配置无效")
	}
	return body, nil
}
func (a *App) restoreLivePolicyConfig(ctx context.Context, body []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(a.adminURL, "/")+"/load", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	client := http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return errors.New("Caddy 配置恢复失败")
	}
	a.confirmRestoredApplication(ctx, body)
	return nil
}

func (a *App) saveResolvedPolicy(w http.ResponseWriter, r *http.Request, domain string, p SitePolicy) {
	if p.Mode == "none" || p.Mode == "inherit" {
		p = SitePolicy{Mode: p.Mode}
	}
	if err := validateSitePolicy(p); err != nil {
		fail(w, 400, err)
		return
	}
	p.Inherit = false
	// Old upstream-header defaults are retained for exports but are no longer inherited.
	p.BasicAuthOn = false
	p.BasicAuthUser = ""
	a.mu.Lock()
	defer a.mu.Unlock()
	impact, err := a.policyImpact(r.Context(), domain, p)
	if err != nil {
		fail(w, 500, err)
		return
	}
	key := securityGlobalKey
	global, domains := updatedPolicies(a.readSecurityGlobal(r.Context()), a.readSecurityDomains(r.Context()), domain, p)
	var value any = global
	if domain != "" {
		key = securityDomainsKey
		value = domains
	}
	var old string
	scanErr := a.db.QueryRowContext(r.Context(), `SELECT value FROM panel_settings WHERE key=?`, key).Scan(&old)
	if scanErr != nil && !errors.Is(scanErr, sql.ErrNoRows) {
		fail(w, 500, scanErr)
		return
	}
	raw, err := json.Marshal(value)
	if err != nil {
		fail(w, 500, err)
		return
	}
	var liveConfig []byte
	if impact.Updated > 0 {
		liveConfig, err = a.livePolicyConfig(r.Context())
		if err != nil {
			fail(w, 502, errors.New("无法读取当前引擎配置，未修改策略。"))
			return
		}
	}
	if err := writePanelSetting(r.Context(), a, key, string(raw)); err != nil {
		fail(w, 500, err)
		return
	}
	if impact.Updated > 0 {
		if err := a.applyLocked(r.Context()); err != nil {
			// Restore even when the browser disconnected while Caddy was validating.
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			var restoreErr error
			if errors.Is(scanErr, sql.ErrNoRows) {
				_, restoreErr = a.db.ExecContext(ctx, `DELETE FROM panel_settings WHERE key=?`, key)
			} else {
				restoreErr = writePanelSetting(ctx, a, key, old)
			}
			if restoreErr != nil {
				fail(w, 500, errors.New("应用失败且策略恢复失败，请查看程序日志与存储状态"))
				return
			}
			if err := a.restoreLivePolicyConfig(ctx, liveConfig); err != nil {
				fail(w, 502, errors.New("应用失败，已恢复原策略，但引擎恢复未确认，请查看程序日志。"))
				return
			}
			fail(w, 502, errors.New("保存并应用失败，已恢复原策略和原引擎配置。"))
			return
		}
	}
	writeJSON(w, 200, impact)
}
