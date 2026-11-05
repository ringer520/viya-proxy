package main

import (
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
)

func mustPrefix(t *testing.T, raw string) *net.IPNet {
	t.Helper()
	_, network, err := net.ParseCIDR(raw)
	if err != nil {
		t.Fatalf("bad prefix %s: %v", raw, err)
	}
	return network
}

func guardApp(t *testing.T, rules panelAccessRules) *App {
	t.Helper()
	a := &App{trustedProxies: []*net.IPNet{mustPrefix(t, "127.0.0.1/32"), mustPrefix(t, "::1/128")}}
	a.panelAccessCache.Store(rules)
	return a
}

func runGuard(a *App, r *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	a.panelAccessGuard(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})).ServeHTTP(rec, r)
	return rec
}

func TestNormalizePanelHost(t *testing.T) {
	cases := map[string]string{
		"https://Viya.Example.com:443.": "viya.example.com",
		"viya.example.com:8181":         "viya.example.com",
		"  VIYA.example.com  ":          "viya.example.com",
		"viya.example.com/path":         "viya.example.com",
		"[::1]:8181":                    "::1",
		"*.example.com":                 "*.example.com",
	}
	for raw, want := range cases {
		if got := normalizePanelHost(raw); got != want {
			t.Errorf("normalizePanelHost(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestPanelHostAllowed(t *testing.T) {
	allowed := []string{"viya.example.com", "*.internal.test"}
	cases := []struct {
		host string
		want bool
	}{
		{"viya.example.com", true},
		{"VIYA.example.com:8181", true},
		{"viya.example.com.", true},
		{"other.example.com", false},
		{"a.internal.test", true},
		{"internal.test", false}, // the wildcard covers subdomains only
		{"1.2.3.4:8181", false},
		{"", false},
	}
	for _, c := range cases {
		if got := panelHostAllowed(allowed, c.host); got != c.want {
			t.Errorf("panelHostAllowed(%q) = %v, want %v", c.host, got, c.want)
		}
	}
}

func TestPanelIPAllowed(t *testing.T) {
	allowed := []string{"203.0.113.7/32", "198.51.100.0/24", "2001:db8::/32"}
	cases := map[string]bool{
		"203.0.113.7":   true,
		"203.0.113.8":   false,
		"198.51.100.42": true,
		"2001:db8::5":   true,
		"2001:db9::5":   false,
		"not-an-ip":     false,
	}
	for ip, want := range cases {
		if got := panelIPAllowed(allowed, ip); got != want {
			t.Errorf("panelIPAllowed(%q) = %v, want %v", ip, got, want)
		}
	}
}

func TestValidatePanelAccess(t *testing.T) {
	rules, err := validatePanelAccess(panelAccessRules{
		Hosts:     []string{"https://Viya.Example.com:443", "viya.example.com", "*.internal.test"},
		IPs:       []string{"203.0.113.7", "198.51.100.0/24", "2001:db8::1", "203.0.113.7"},
		HTTPSOnly: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rules.Hosts) != 2 || rules.Hosts[0] != "viya.example.com" || rules.Hosts[1] != "*.internal.test" {
		t.Fatalf("hosts not normalized/deduplicated: %v", rules.Hosts)
	}
	if len(rules.IPs) != 3 {
		t.Fatalf("ips not normalized/deduplicated: %v", rules.IPs)
	}
	if rules.IPs[0] != "203.0.113.7/32" || rules.IPs[2] != "2001:db8::1/128" {
		t.Fatalf("addresses not canonical: %v", rules.IPs)
	}
	if !rules.HTTPSOnly {
		t.Fatal("https_only dropped")
	}

	for _, bad := range []panelAccessRules{
		{Hosts: []string{"exa mple.com"}},
		{Hosts: []string{"*"}},
		{Hosts: []string{"http://"}},
		{IPs: []string{"999.1.1.1"}},
		{IPs: []string{"10.0.0.0/33"}},
	} {
		if _, err := validatePanelAccess(bad); err == nil {
			t.Fatalf("invalid rules accepted: %+v", bad)
		}
	}
}

func TestPanelAccessGuardLoopbackAlwaysPasses(t *testing.T) {
	a := guardApp(t, panelAccessRules{Hosts: []string{"viya.example.com"}, IPs: []string{"203.0.113.0/24"}, HTTPSOnly: true})
	r := httptest.NewRequest("GET", "http://127.0.0.1:8181/", nil)
	r.Host = "127.0.0.1:8181"
	r.RemoteAddr = "127.0.0.1:51234"
	if rec := runGuard(a, r); rec.Code != http.StatusOK {
		t.Fatalf("loopback tunnel blocked: %d %s", rec.Code, rec.Body.String())
	}
}

func TestPanelAccessGuardRejectsForeignHost(t *testing.T) {
	a := guardApp(t, panelAccessRules{Hosts: []string{"viya.example.com"}})
	// Behind Caddy: the peer is loopback but the forwarded client is not, so the
	// allowlist must still apply.
	r := httptest.NewRequest("GET", "http://203.0.113.9:8181/", nil)
	r.Header.Set("X-Forwarded-For", "203.0.113.9")
	r.RemoteAddr = "127.0.0.1:40000"
	if rec := runGuard(a, r); rec.Code != http.StatusForbidden {
		t.Fatalf("raw IP host accepted: %d", rec.Code)
	}
}

func TestPanelAccessGuardAllowsConfiguredHost(t *testing.T) {
	a := guardApp(t, panelAccessRules{Hosts: []string{"viya.example.com"}})
	r := httptest.NewRequest("GET", "http://viya.example.com/", nil)
	r.Header.Set("X-Forwarded-For", "203.0.113.9")
	r.RemoteAddr = "127.0.0.1:40000"
	if rec := runGuard(a, r); rec.Code != http.StatusOK {
		t.Fatalf("configured host blocked: %d %s", rec.Code, rec.Body.String())
	}
}

func TestPanelAccessGuardIgnoresSpoofedForwardingFromPublicPeer(t *testing.T) {
	a := guardApp(t, panelAccessRules{Hosts: []string{"viya.example.com"}})
	// A public peer is not a trusted proxy, so its X-Forwarded-For must not be
	// read as a loopback client and unlock the bypass.
	r := httptest.NewRequest("GET", "http://203.0.113.9:8181/", nil)
	r.Header.Set("X-Forwarded-For", "127.0.0.1")
	r.RemoteAddr = "203.0.113.9:5555"
	if rec := runGuard(a, r); rec.Code != http.StatusForbidden {
		t.Fatalf("spoofed forwarding bypassed the allowlist: %d", rec.Code)
	}
}

func TestPanelAccessGuardHTTPSOnlyRedirect(t *testing.T) {
	a := guardApp(t, panelAccessRules{HTTPSOnly: true})
	r := httptest.NewRequest("GET", "http://viya.example.com/sites?tab=1", nil)
	r.Header.Set("X-Forwarded-For", "203.0.113.9")
	r.RemoteAddr = "127.0.0.1:40000"
	rec := runGuard(a, r)
	if rec.Code != http.StatusPermanentRedirect {
		t.Fatalf("expected 308, got %d", rec.Code)
	}
	if got := rec.Header().Get("Location"); got != "https://viya.example.com/sites?tab=1" {
		t.Fatalf("unexpected redirect target: %q", got)
	}

	// A proxy that reports HTTPS must not be redirected.
	same := httptest.NewRequest("GET", "http://viya.example.com/", nil)
	same.Header.Set("X-Forwarded-For", "203.0.113.9")
	same.Header.Set("X-Forwarded-Proto", "https")
	same.RemoteAddr = "127.0.0.1:40000"
	if code := runGuard(a, same).Code; code != http.StatusOK {
		t.Fatalf("https request redirected: %d", code)
	}
}

func TestPanelAccessGuardDisabledWhenEmpty(t *testing.T) {
	a := guardApp(t, panelAccessRules{})
	r := httptest.NewRequest("GET", "http://203.0.113.9:8181/", nil)
	r.RemoteAddr = "203.0.113.9:5555"
	if rec := runGuard(a, r); rec.Code != http.StatusOK {
		t.Fatalf("empty rules blocked traffic: %d", rec.Code)
	}
}

func TestIsLocalAddressOnlyMatchesThisHost(t *testing.T) {
	a := &App{}
	if a.isLocalAddress(netip.MustParseAddr("203.0.113.9")) {
		t.Fatal("public address treated as local")
	}
	if !a.isLocalAddress(netip.MustParseAddr("127.0.0.1")) {
		t.Fatal("loopback not recognised as a local interface address")
	}
}
