package main

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestApplySecurityDefaultsRollsBackPartialUpdate(t *testing.T) {
	app, db := newTestApp(t)
	defer db.Close()
	_, err := db.Exec(`CREATE TABLE proxy_hosts(id INTEGER PRIMARY KEY,domain TEXT,security_headers TEXT,custom_headers TEXT,ip_allowlist TEXT,ip_denylist TEXT,rate_limit_requests INTEGER,rate_limit_window TEXT,max_body_bytes INTEGER,allowed_methods TEXT,hide_server INTEGER,version INTEGER,updated_at TEXT);
	INSERT INTO proxy_hosts(id,domain,security_headers,version) VALUES(1,'one.example.com','{"inherit":true}',1),(2,'two.example.com','{"inherit":true}',1);
	CREATE TRIGGER refuse_second BEFORE UPDATE ON proxy_hosts WHEN OLD.id=2 BEGIN SELECT RAISE(ABORT,'simulated storage failure'); END;`)
	if err != nil {
		t.Fatal(err)
	}
	saveSecuritySetting(t, app, securityDomainsKey, map[string]SitePolicy{"example.com": {Enabled: true, NoSniff: true}})
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/", strings.NewReader(`{"domain":"example.com","scope":"inherited"}`))
	app.applySecurityDefaults(w, r)
	if w.Code != 500 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var changed int
	if err := db.QueryRow(`SELECT COUNT(*) FROM proxy_hosts WHERE version<>1`).Scan(&changed); err != nil {
		t.Fatal(err)
	}
	if changed != 0 {
		t.Fatalf("partial policy changes persisted: %d", changed)
	}
}

func saveSecuritySetting(t *testing.T, app *App, key string, value any) {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.db.Exec(`INSERT INTO panel_settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, string(raw)); err != nil {
		t.Fatal(err)
	}
}

func TestDomainUnder(t *testing.T) {
	cases := []struct {
		child, parent string
		want          bool
	}{
		{"a.example.com", "example.com", true},
		{"x.a.example.com", "example.com", true},
		{"example.com", "example.com", false},
		{"notexample.com", "example.com", false},
		{"a.example.org", "example.com", false},
	}
	for _, c := range cases {
		if got := domainUnder(c.child, c.parent); got != c.want {
			t.Fatalf("domainUnder(%q,%q) = %v, want %v", c.child, c.parent, got, c.want)
		}
	}
}

func TestRenderSecurityHeaders(t *testing.T) {
	if got := renderSecurityHeaders(SecurityHeaders{}); got != nil {
		t.Fatalf("disabled policy should render nothing, got %v", got)
	}
	sh := SecurityHeaders{
		Enabled:        true,
		FrameOptions:   "SAMEORIGIN",
		NoSniff:        true,
		HSTS:           true,
		ReferrerPolicy: "strict-origin-when-cross-origin",
		CSP:            "default-src 'self'",
	}
	got := renderSecurityHeaders(sh)
	if v := got["X-Frame-Options"]; len(v) != 1 || v[0] != "SAMEORIGIN" {
		t.Fatalf("frame options missing: %v", got)
	}
	if v := got["X-Content-Type-Options"]; len(v) != 1 || v[0] != "nosniff" {
		t.Fatalf("nosniff missing: %v", got)
	}
	if v := got["Strict-Transport-Security"]; len(v) != 1 || v[0] != "max-age=31536000" {
		t.Fatalf("hsts missing: %v", got)
	}
	if v := got["Referrer-Policy"]; len(v) != 1 || v[0] != "strict-origin-when-cross-origin" {
		t.Fatalf("referrer policy missing: %v", got)
	}
	if v := got["Content-Security-Policy"]; len(v) != 1 || v[0] != "default-src 'self'" {
		t.Fatalf("csp missing: %v", got)
	}
}

func TestRenderSecurityHeadersSkipsInvalidValues(t *testing.T) {
	sh := SecurityHeaders{Enabled: true, FrameOptions: "ALLOW", ReferrerPolicy: "bogus"}
	if got := renderSecurityHeaders(sh); got != nil {
		t.Fatalf("invalid values should be dropped, got %v", got)
	}
}

func TestValidateSecurityHeaders(t *testing.T) {
	if err := validateSecurityHeaders(SecurityHeaders{}); err != nil {
		t.Fatalf("empty policy should be valid: %v", err)
	}
	if err := validateSecurityHeaders(SecurityHeaders{FrameOptions: "DENY"}); err != nil {
		t.Fatalf("DENY should be valid: %v", err)
	}
	if err := validateSecurityHeaders(SecurityHeaders{FrameOptions: "ALLOW"}); err == nil {
		t.Fatal("ALLOW should be rejected")
	}
	if err := validateSecurityHeaders(SecurityHeaders{ReferrerPolicy: "wat"}); err == nil {
		t.Fatal("unknown referrer policy should be rejected")
	}
	if err := validateSecurityHeaders(SecurityHeaders{CSP: "a\nb"}); err == nil {
		t.Fatal("multi-line CSP should be rejected")
	}
}

func TestResolveSitePolicyFallsBackToGlobal(t *testing.T) {
	app, db := newTestApp(t)
	defer db.Close()
	saveSecuritySetting(t, app, securityGlobalKey, SecurityHeaders{Enabled: true, HSTS: true})

	got := app.resolveSitePolicy(context.Background(), "a.example.com")
	if !got.Enabled || !got.HSTS {
		t.Fatalf("expected the global policy, got %+v", got)
	}
}

func TestResolveSitePolicyDomainBeatsGlobal(t *testing.T) {
	app, db := newTestApp(t)
	defer db.Close()
	saveSecuritySetting(t, app, securityGlobalKey, SecurityHeaders{Enabled: true, HSTS: true})
	saveSecuritySetting(t, app, securityDomainsKey, map[string]SecurityHeaders{
		"example.com": {Enabled: true, NoSniff: true},
	})

	got := app.resolveSitePolicy(context.Background(), "a.example.com")
	if !got.NoSniff || got.HSTS {
		t.Fatalf("expected the domain policy to win, got %+v", got)
	}
}

func TestResolveSitePolicyUsesLongestMatch(t *testing.T) {
	app, db := newTestApp(t)
	defer db.Close()
	saveSecuritySetting(t, app, securityDomainsKey, map[string]SecurityHeaders{
		"example.com":   {Enabled: true, HSTS: true},
		"a.example.com": {Enabled: true, NoSniff: true},
	})

	got := app.resolveSitePolicy(context.Background(), "x.a.example.com")
	if !got.NoSniff || got.HSTS {
		t.Fatalf("longest match should win, got %+v", got)
	}
}

func TestApplyPolicyInheritanceSetsPolicy(t *testing.T) {
	app, db := newTestApp(t)
	defer db.Close()
	saveSecuritySetting(t, app, securityGlobalKey, SecurityHeaders{Enabled: true, HSTS: true})

	h := &Host{Domain: "new.example.com"}
	app.applyPolicyInheritance(context.Background(), h)
	if !h.SecurityHeaders.Enabled || !h.SecurityHeaders.Inherit {
		t.Fatalf("expected inherited policy, got %+v", h.SecurityHeaders)
	}
}

func TestApplyPolicyInheritanceRespectsExplicitChoice(t *testing.T) {
	app, db := newTestApp(t)
	defer db.Close()
	saveSecuritySetting(t, app, securityGlobalKey, SecurityHeaders{Enabled: true, HSTS: true})

	h := &Host{Domain: "new.example.com", SecurityHeaders: SecurityHeaders{Enabled: true, NoSniff: true}}
	app.applyPolicyInheritance(context.Background(), h)
	if h.SecurityHeaders.HSTS || !h.SecurityHeaders.NoSniff || h.SecurityHeaders.Inherit {
		t.Fatalf("explicit policy should be kept, got %+v", h.SecurityHeaders)
	}
}

func TestApplyPolicyInheritanceCarriesAccessControls(t *testing.T) {
	app, db := newTestApp(t)
	defer db.Close()
	saveSecuritySetting(t, app, securityGlobalKey, SitePolicy{
		Enabled:       true,
		NoSniff:       true,
		CustomHeaders: map[string]string{"X-Env": "prod"},
		IPAllowlist:   []string{"10.0.0.0/8"},
		IPDenylist:    []string{"1.2.3.4"},
		RateLimit:     100,
		RateLimitWin:  "1m",
		BasicAuthUser: "shared",
	})
	h := &Host{Domain: "a.example.com"}
	app.applyPolicyInheritance(context.Background(), h)
	if h.CustomHeaders["X-Env"] != "prod" {
		t.Fatalf("custom headers not inherited: %+v", h.CustomHeaders)
	}
	if len(h.IPAllowlist) != 1 || len(h.IPDenylist) != 1 {
		t.Fatalf("IP filters not inherited: %+v / %+v", h.IPAllowlist, h.IPDenylist)
	}
	if h.RateLimitRequests != 100 || h.RateLimitWindow != "1m" {
		t.Fatalf("rate limit not inherited: %d %q", h.RateLimitRequests, h.RateLimitWindow)
	}
	if h.BasicAuthUser != "shared" {
		t.Fatalf("basic auth user not inherited: %q", h.BasicAuthUser)
	}
	if h.BasicAuthEnabled {
		t.Fatal("basic auth must not auto-enable without a password")
	}
	if !h.SecurityHeaders.Inherit || !h.SecurityHeaders.Enabled {
		t.Fatalf("policy should be marked as inherited: %+v", h.SecurityHeaders)
	}
}

func TestValidateSitePolicy(t *testing.T) {
	if err := validateSitePolicy(SitePolicy{Enabled: true, RateLimitWin: "5m"}); err == nil {
		t.Fatal("unsupported window should be rejected")
	}
	if err := validateSitePolicy(SitePolicy{Enabled: true, RateLimit: 10, RateLimitWin: "1m"}); err != nil {
		t.Fatalf("valid policy rejected: %v", err)
	}
	if err := validateSitePolicy(SitePolicy{Enabled: true, RateLimit: -1}); err == nil {
		t.Fatal("negative rate limit should be rejected")
	}
}
