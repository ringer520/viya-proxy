package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

// Two panel_settings keys carry the inherited policy: a panel-wide baseline
// that every new site picks up, and a per-root-domain override keyed by the
// registrable domain that owns it.
const (
	securityGlobalKey  = "security_global_defaults"
	securityDomainsKey = "security_domains_defaults"
)

// SitePolicy is the inheritable bundle of per-site controls: security response
// headers, basic auth identity, upstream request headers, IP filters and rate
// limiting. Fields are flat so policies written by the earlier header-only
// release still decode cleanly.
type SitePolicy struct {
	Enabled        bool              `json:"enabled"`
	Inherit        bool              `json:"inherit"`
	FrameOptions   string            `json:"frame_options,omitempty"`
	NoSniff        bool              `json:"nosniff"`
	HSTS           bool              `json:"hsts"`
	ReferrerPolicy string            `json:"referrer_policy,omitempty"`
	CSP            string            `json:"csp,omitempty"`
	BasicAuthUser  string            `json:"basic_auth_user,omitempty"`
	BasicAuthOn    bool              `json:"basic_auth_enabled"`
	CustomHeaders  map[string]string `json:"custom_headers,omitempty"`
	IPAllowlist    []string          `json:"ip_allowlist,omitempty"`
	IPDenylist     []string          `json:"ip_denylist,omitempty"`
	RateLimit      int               `json:"rate_limit_requests,omitempty"`
	RateLimitWin   string            `json:"rate_limit_window,omitempty"`
	MaxBodyBytes   int64             `json:"max_body_bytes,omitempty"`
	AllowedMethods []string          `json:"allowed_methods,omitempty"`
	HideServer     bool              `json:"hide_server"`
}

// securityDefaultsPayload is what the settings UI reads in one round trip.
type securityDefaultsPayload struct {
	Global  SitePolicy            `json:"global"`
	Domains map[string]SitePolicy `json:"domains"`
}

func canonicalSiteDomain(domain string) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(domain), "."))
}

// domainUnder reports whether child sits below parent. The leading dot keeps
// "notexample.com" from matching "example.com".
func domainUnder(child, parent string) bool {
	return child != parent && strings.HasSuffix(child, "."+parent)
}

func (a *App) readSecurityGlobal(ctx context.Context) SitePolicy {
	var p SitePolicy
	var raw string
	if err := a.db.QueryRowContext(ctx, `SELECT value FROM panel_settings WHERE key=?`, securityGlobalKey).Scan(&raw); err == nil && raw != "" {
		_ = json.Unmarshal([]byte(raw), &p)
	}
	return p
}

func (a *App) readSecurityDomains(ctx context.Context) map[string]SitePolicy {
	out := map[string]SitePolicy{}
	var raw string
	if err := a.db.QueryRowContext(ctx, `SELECT value FROM panel_settings WHERE key=?`, securityDomainsKey).Scan(&raw); err == nil && raw != "" {
		_ = json.Unmarshal([]byte(raw), &out)
	}
	if out == nil {
		out = map[string]SitePolicy{}
	}
	return out
}

// resolveSitePolicy walks the domain suffix chain from the most specific label
// down to the registrable domain, returning the first enabled policy it finds
// and falling back to the panel-wide default.
func (a *App) resolveSitePolicy(ctx context.Context, domain string) SitePolicy {
	candidate := canonicalSiteDomain(domain)
	domains := a.readSecurityDomains(ctx)
	for strings.Contains(candidate, ".") {
		if p, ok := domains[candidate]; ok && p.Enabled {
			return p
		}
		candidate = candidate[strings.Index(candidate, ".")+1:]
	}
	return a.readSecurityGlobal(ctx)
}

// applyPolicyInheritance fills a new site from its parent policy when the
// caller supplied nothing at all. An explicit choice always wins. Basic auth is
// carried over as a username only: the password cannot be inherited, so the
// switch stays off until the operator sets one.
func (a *App) applyPolicyInheritance(ctx context.Context, h *Host) {
	if h.SecurityHeaders.Enabled || h.SecurityHeaders.Inherit {
		return
	}
	p := a.resolveSitePolicy(ctx, h.Domain)
	if !p.Enabled {
		return
	}
	h.SecurityHeaders = SecurityHeaders{
		Enabled:        true,
		Inherit:        true,
		FrameOptions:   p.FrameOptions,
		NoSniff:        p.NoSniff,
		HSTS:           p.HSTS,
		ReferrerPolicy: p.ReferrerPolicy,
		CSP:            p.CSP,
	}
	if p.BasicAuthUser != "" && h.BasicAuthUser == "" {
		h.BasicAuthUser = p.BasicAuthUser
	}
	if len(p.CustomHeaders) > 0 && len(h.CustomHeaders) == 0 {
		h.CustomHeaders = p.CustomHeaders
	}
	if len(p.IPAllowlist) > 0 && len(h.IPAllowlist) == 0 {
		h.IPAllowlist = p.IPAllowlist
	}
	if len(p.IPDenylist) > 0 && len(h.IPDenylist) == 0 {
		h.IPDenylist = p.IPDenylist
	}
	if p.RateLimit > 0 && h.RateLimitRequests == 0 {
		h.RateLimitRequests = p.RateLimit
		if p.RateLimitWin != "" {
			h.RateLimitWindow = p.RateLimitWin
		}
	}
	if p.MaxBodyBytes > 0 && h.MaxBodyBytes == 0 {
		h.MaxBodyBytes = p.MaxBodyBytes
	}
	if len(p.AllowedMethods) > 0 && len(h.AllowedMethods) == 0 {
		h.AllowedMethods = p.AllowedMethods
	}
	if p.HideServer {
		h.HideServer = true
	}
}

// renderSecurityHeaders turns a policy's header fields into Caddy's
// headers-module response set.
func renderSecurityHeaders(sh SecurityHeaders) map[string][]string {
	if !sh.Enabled {
		return nil
	}
	set := map[string][]string{}
	switch sh.FrameOptions {
	case "SAMEORIGIN", "DENY":
		set["X-Frame-Options"] = []string{sh.FrameOptions}
	}
	if sh.NoSniff {
		set["X-Content-Type-Options"] = []string{"nosniff"}
	}
	if sh.HSTS {
		set["Strict-Transport-Security"] = []string{"max-age=31536000"}
	}
	switch sh.ReferrerPolicy {
	case "no-referrer", "strict-origin-when-cross-origin", "same-origin", "origin":
		set["Referrer-Policy"] = []string{sh.ReferrerPolicy}
	}
	if csp := strings.TrimSpace(sh.CSP); csp != "" {
		set["Content-Security-Policy"] = []string{csp}
	}
	if len(set) == 0 {
		return nil
	}
	return set
}

func validateSecurityHeaders(sh SecurityHeaders) error {
	switch sh.FrameOptions {
	case "", "SAMEORIGIN", "DENY":
	default:
		return errors.New("X-Frame-Options must be SAMEORIGIN or DENY")
	}
	switch sh.ReferrerPolicy {
	case "", "no-referrer", "strict-origin-when-cross-origin", "same-origin", "origin":
	default:
		return errors.New("unsupported Referrer-Policy value")
	}
	if len(sh.CSP) > 4096 {
		return errors.New("Content-Security-Policy is too long")
	}
	if strings.ContainsAny(sh.CSP, "\r\n") {
		return errors.New("Content-Security-Policy must be a single line")
	}
	return nil
}

func validateSitePolicy(p SitePolicy) error {
	if err := validateSecurityHeaders(SecurityHeaders{
		Enabled:        p.Enabled,
		FrameOptions:   p.FrameOptions,
		NoSniff:        p.NoSniff,
		HSTS:           p.HSTS,
		ReferrerPolicy: p.ReferrerPolicy,
		CSP:            p.CSP,
	}); err != nil {
		return err
	}
	switch p.RateLimitWin {
	case "", "1s", "10s", "1m", "1h":
	default:
		return errors.New("unsupported rate limit window")
	}
	if p.RateLimit < 0 || p.RateLimit > 1000000 {
		return errors.New("rate limit must be between 0 and 1000000")
	}
	if err := validateHeaders(p.CustomHeaders); err != nil {
		return err
	}
	for _, list := range [][]string{p.IPAllowlist, p.IPDenylist} {
		if len(list) > 64 {
			return errors.New("at most 64 IP entries are allowed")
		}
	}
	if p.MaxBodyBytes < 0 || p.MaxBodyBytes > 10<<30 {
		return errors.New("request body limit must be between 0 and 10 GiB")
	}
	for _, method := range p.AllowedMethods {
		switch method {
		case "GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS":
		default:
			return errors.New("unsupported HTTP method: " + method)
		}
	}
	return nil
}

func writePanelSetting(ctx context.Context, a *App, key string, value string) error {
	_, err := a.db.ExecContext(ctx, `INSERT INTO panel_settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	return err
}

func (a *App) getSecurityDefaults(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, securityDefaultsPayload{
		Global:  a.readSecurityGlobal(r.Context()),
		Domains: a.readSecurityDomains(r.Context()),
	})
}

func (a *App) saveSecurityGlobal(w http.ResponseWriter, r *http.Request) {
	var p SitePolicy
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&p); err != nil {
		fail(w, http.StatusBadRequest, errors.New("invalid JSON body"))
		return
	}
	if err := validateSitePolicy(p); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	p.Inherit = false
	raw, _ := json.Marshal(p)
	if err := writePanelSetting(r.Context(), a, securityGlobalKey, string(raw)); err != nil {
		fail(w, http.StatusInternalServerError, errors.New("could not save the global security policy"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *App) saveSecurityDomain(w http.ResponseWriter, r *http.Request) {
	domain := canonicalSiteDomain(r.PathValue("domain"))
	if domain == "" || !strings.Contains(domain, ".") {
		fail(w, http.StatusBadRequest, errors.New("invalid root domain"))
		return
	}
	var p SitePolicy
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&p); err != nil {
		fail(w, http.StatusBadRequest, errors.New("invalid JSON body"))
		return
	}
	if err := validateSitePolicy(p); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	p.Inherit = false
	domains := a.readSecurityDomains(r.Context())
	if p.Enabled {
		domains[domain] = p
	} else {
		delete(domains, domain)
	}
	raw, _ := json.Marshal(domains)
	if err := writePanelSetting(r.Context(), a, securityDomainsKey, string(raw)); err != nil {
		fail(w, http.StatusInternalServerError, errors.New("could not save the domain security policy"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *App) deleteSecurityDomain(w http.ResponseWriter, r *http.Request) {
	domain := canonicalSiteDomain(r.PathValue("domain"))
	domains := a.readSecurityDomains(r.Context())
	delete(domains, domain)
	raw, _ := json.Marshal(domains)
	if err := writePanelSetting(r.Context(), a, securityDomainsKey, string(raw)); err != nil {
		fail(w, http.StatusInternalServerError, errors.New("could not remove the domain security policy"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

type applySecurityInput struct {
	Domain string `json:"domain"`
	Scope  string `json:"scope"`
}

// applySecurityDefaults pushes a root domain's policy into its existing sites.
// scope="inherited" only touches rows that still follow their parent, which
// keeps hand-tuned sites from being silently overwritten. Basic auth is never
// rewritten here: the password cannot travel with the policy.
func (a *App) applySecurityDefaults(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	var input applySecurityInput
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&input); err != nil {
		fail(w, http.StatusBadRequest, errors.New("invalid JSON body"))
		return
	}
	root := canonicalSiteDomain(input.Domain)
	if root == "" {
		fail(w, http.StatusBadRequest, errors.New("domain is required"))
		return
	}
	policy, ok := a.readSecurityDomains(r.Context())[root]
	if !ok || !policy.Enabled {
		fail(w, http.StatusBadRequest, errors.New("this domain has no default policy to apply"))
		return
	}
	headers := SecurityHeaders{
		Enabled:        true,
		Inherit:        true,
		FrameOptions:   policy.FrameOptions,
		NoSniff:        policy.NoSniff,
		HSTS:           policy.HSTS,
		ReferrerPolicy: policy.ReferrerPolicy,
		CSP:            policy.CSP,
	}
	headersJSON, _ := json.Marshal(headers)
	methodsJSON, _ := json.Marshal(policy.AllowedMethods)
	customJSON, _ := json.Marshal(policy.CustomHeaders)
	allowJSON, _ := json.Marshal(policy.IPAllowlist)
	denyJSON, _ := json.Marshal(policy.IPDenylist)
	if policy.CustomHeaders == nil {
		customJSON = []byte("{}")
	}
	if policy.IPAllowlist == nil {
		allowJSON = []byte("[]")
	}
	if policy.IPDenylist == nil {
		denyJSON = []byte("[]")
	}

	rows, err := a.db.QueryContext(r.Context(), `SELECT id,domain,security_headers FROM proxy_hosts`)
	if err != nil {
		fail(w, http.StatusInternalServerError, errors.New("could not read sites"))
		return
	}
	var ids []int64
	for rows.Next() {
		var id int64
		var domain, stored string
		if rows.Scan(&id, &domain, &stored) != nil {
			continue
		}
		if !domainUnder(canonicalSiteDomain(domain), root) {
			continue
		}
		if input.Scope != "all" {
			var existing SecurityHeaders
			_ = json.Unmarshal([]byte(stored), &existing)
			if !existing.Inherit {
				continue
			}
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		fail(w, http.StatusInternalServerError, errors.New("could not read sites"))
		return
	}

	tx, err := a.db.BeginTx(r.Context(), nil)
	if err != nil {
		fail(w, http.StatusInternalServerError, errors.New("could not begin policy update"))
		return
	}
	defer tx.Rollback()
	for _, id := range ids {
		if _, err := tx.ExecContext(r.Context(),
			`UPDATE proxy_hosts SET security_headers=?,custom_headers=?,ip_allowlist=?,ip_denylist=?,rate_limit_requests=?,rate_limit_window=?,max_body_bytes=?,allowed_methods=?,hide_server=?,version=version+1,updated_at=CURRENT_TIMESTAMP WHERE id=?`,
			string(headersJSON), string(customJSON), string(allowJSON), string(denyJSON), policy.RateLimit, policy.RateLimitWin, policy.MaxBodyBytes, string(methodsJSON), boolInt(policy.HideServer), id); err != nil {
			fail(w, http.StatusInternalServerError, errors.New("could not update a site"))
			return
		}
	}
	if err := tx.Commit(); err != nil {
		fail(w, http.StatusInternalServerError, errors.New("could not save policy updates"))
		return
	}
	if len(ids) > 0 {
		if err := a.applyLocked(r.Context()); err != nil {
			fail(w, http.StatusBadGateway, errors.New("saved but Caddy could not apply the configuration"))
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"updated": len(ids)})
}
