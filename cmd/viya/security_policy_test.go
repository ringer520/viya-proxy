package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPolicyModesAndBoundaries(t *testing.T) {
	global := SitePolicy{Mode: "custom", Enabled: true, HSTS: true}
	domains := map[string]SitePolicy{"example.com": {Mode: "custom", Enabled: false, RateLimit: 80}, "private.example.com": {Mode: "none"}, "inherit.example.com": {Mode: "inherit"}}
	for _, tc := range []struct {
		domain, source string
		rate           int
		enabled        bool
	}{
		{"example.com", "example.com", 80, false},
		{"app.example.com", "example.com", 80, false},
		{"app.private.example.com", "private.example.com", 0, false},
		{"app.inherit.example.com", "example.com", 80, false},
		{"other.org", "global", 0, true},
	} {
		p, source := resolvePolicySource(tc.domain, global, domains)
		if source != tc.source || p.RateLimit != tc.rate || p.Enabled != tc.enabled {
			t.Fatalf("%s: source=%s policy=%+v", tc.domain, source, p)
		}
	}
}

func TestEffectivePolicyKeepsSiteOwnedFields(t *testing.T) {
	h := Host{Domain: "a.example.com", SecurityHeaders: SecurityHeaders{Mode: "follow"}, BasicAuthEnabled: true, BasicAuthUser: "private", CustomHeaders: map[string]string{"X-Local": "keep"}, RateLimitRequests: 7}
	p := SitePolicy{Mode: "custom", IPAllowlist: []string{"10.0.0.0/8"}, RateLimit: 90, CustomHeaders: map[string]string{"X-Default": "must-not-inherit"}, BasicAuthUser: "shared"}
	effectiveHostPolicy(&h, p, nil)
	if h.RateLimitRequests != 90 || len(h.IPAllowlist) != 1 || h.SecurityHeaders.Enabled {
		t.Fatalf("header switch must not disable access policy: %+v", h)
	}
	if !h.BasicAuthEnabled || h.BasicAuthUser != "private" || len(h.CustomHeaders) != 1 || h.CustomHeaders["X-Local"] != "keep" {
		t.Fatalf("site-owned fields changed: %+v", h)
	}
	h.SecurityHeaders.Mode = "none"
	effectiveHostPolicy(&h, p, nil)
	if h.RateLimitRequests != 0 || len(h.IPAllowlist) != 0 || !h.BasicAuthEnabled {
		t.Fatalf("none must clear policy only: %+v", h)
	}
	h.SecurityHeaders = SecurityHeaders{Mode: "custom", Enabled: true, HSTS: true}
	h.RateLimitRequests = 12
	effectiveHostPolicy(&h, p, nil)
	if h.RateLimitRequests != 12 || !h.SecurityHeaders.HSTS {
		t.Fatal("independent site was overwritten")
	}
}

func TestPolicyImpactProtectsNestedAndIndependent(t *testing.T) {
	app, db := newTestApp(t)
	defer db.Close()
	_, err := db.Exec(`CREATE TABLE proxy_hosts(domain TEXT,security_headers TEXT);
 INSERT INTO proxy_hosts VALUES('example.com','{"mode":"follow"}'),('a.example.com','{"inherit":true}'),('local.example.com','{"mode":"custom"}'),('off.example.com','{"mode":"none"}'),('a.child.example.com','{"mode":"follow"}'),('outside.org','{"mode":"follow"}');`)
	if err != nil {
		t.Fatal(err)
	}
	saveSecuritySetting(t, app, securityDomainsKey, map[string]SitePolicy{"example.com": {Mode: "custom", Enabled: true}, "child.example.com": {Mode: "none"}})
	impact, err := app.policyImpact(context.Background(), "example.com", SitePolicy{Mode: "inherit"})
	if err != nil {
		t.Fatal(err)
	}
	if impact.Updated != 2 || impact.Independent != 2 || impact.Overridden != 1 {
		t.Fatalf("incorrect boundary counts: %+v", impact)
	}
	impact, err = app.policyImpact(context.Background(), "", SitePolicy{Mode: "custom", Enabled: true})
	if err != nil || impact.Updated != 1 || impact.Overridden != 3 {
		t.Fatalf("global policy crossed domain boundary: %+v %v", impact, err)
	}
}

func TestPolicySaveRestoresSettingsWhenCompilationFails(t *testing.T) {
	app, db := newTestApp(t)
	defer db.Close()
	// Enough columns for the preview, deliberately missing config-compiler columns.
	_, err := db.Exec(`CREATE TABLE proxy_hosts(domain TEXT,security_headers TEXT);INSERT INTO proxy_hosts VALUES('a.example.com','{"mode":"follow"}');`)
	if err != nil {
		t.Fatal(err)
	}
	restoredLive := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			restoredLive = true
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{}`))
	}))
	defer server.Close()
	app.adminURL = server.URL
	original := SitePolicy{Mode: "custom", Enabled: true, NoSniff: true}
	saveSecuritySetting(t, app, securityGlobalKey, original)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("PUT", "/", strings.NewReader(`{"mode":"custom","enabled":true,"hsts":true}`))
	app.saveSecurityGlobal(w, r)
	if !restoredLive {
		t.Fatal("original live configuration was not restored")
	}
	if w.Code != 502 {
		t.Fatalf("expected apply failure, got %d %s", w.Code, w.Body.String())
	}
	restored := app.readSecurityGlobal(context.Background())
	before, _ := json.Marshal(original)
	after, _ := json.Marshal(restored)
	if string(before) != string(after) {
		t.Fatalf("original policy was not restored: %s", after)
	}
}

func TestPolicyFutureOnlyAndExplicitNone(t *testing.T) {
	app, db := newTestApp(t)
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE proxy_hosts(domain TEXT,security_headers TEXT);`); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("PUT", "/", strings.NewReader(`{"mode":"custom","enabled":false,"rate_limit_requests":25,"rate_limit_window":"1m"}`))
	request.SetPathValue("domain", "example.com")
	response := httptest.NewRecorder()
	app.saveSecurityDomain(response, request)
	if response.Code != 200 {
		t.Fatalf("future-only policy failed: %d %s", response.Code, response.Body.String())
	}
	policy := app.resolveSitePolicy(context.Background(), "app.example.com")
	if policy.Enabled || policy.RateLimit != 25 {
		t.Fatalf("headers-off access policy lost: %+v", policy)
	}
	host := Host{Domain: "app.example.com", SecurityHeaders: SecurityHeaders{Mode: "none"}}
	app.prepareHostPolicy(context.Background(), &host)
	if host.RateLimitRequests != 0 || host.SecurityHeaders.Inherit || host.SecurityHeaders.Mode != "none" {
		t.Fatal("explicit none inherited a default")
	}
}

func TestPolicyModeIgnoresInactiveDraftAndRejectsMalformedIP(t *testing.T) {
	app, db := newTestApp(t)
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE proxy_hosts(domain TEXT,security_headers TEXT)`); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		body   string
		status int
	}{
		{`{"policy":{"mode":"none","rate_limit_requests":-9,"ip_allowlist":["not-an-ip"]}}`, 200},
		{`{"policy":{"mode":"custom","ip_allowlist":["not-an-ip"]}}`, 400},
	} {
		response := httptest.NewRecorder()
		request := httptest.NewRequest("POST", "/", strings.NewReader(test.body))
		app.previewSecurityPolicy(response, request)
		if response.Code != test.status {
			t.Fatalf("status=%d expected %d: %s", response.Code, test.status, response.Body.String())
		}
	}
}
