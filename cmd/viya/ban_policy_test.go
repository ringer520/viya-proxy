package main

import (
	"net"
	"net/http/httptest"
	"testing"
)

func TestDefaultBanPolicy(t *testing.T) {
	p := defaultBanPolicy()
	if !p.Enabled || p.Threshold != 20 || p.WindowMin != 5 || p.BanMin != 15 {
		t.Fatalf("unexpected defaults: %+v", p)
	}
}

func TestBanPolicyAllows(t *testing.T) {
	p := banPolicy{Allowlist: []string{"192.168.1.10", "10.0.0.0/8", "", "not-an-ip"}}
	cases := []struct {
		ip   string
		want bool
	}{
		{"192.168.1.10", true},
		{"10.1.2.3", true},
		{"192.168.1.11", false},
		{"8.8.8.8", false},
	}
	for _, c := range cases {
		if got := p.allows(net.ParseIP(c.ip)); got != c.want {
			t.Fatalf("allows(%s) = %v, want %v", c.ip, got, c.want)
		}
	}
}

func TestReadBanPolicyDefaultsWhenMissing(t *testing.T) {
	app, db := newTestApp(t)
	defer db.Close()
	p := app.readBanPolicy()
	if !p.Enabled || p.Threshold != 20 {
		t.Fatalf("expected defaults, got %+v", p)
	}
}

func TestReadBanPolicyFallsBackOnInvalidJSON(t *testing.T) {
	app, db := newTestApp(t)
	defer db.Close()
	if _, err := db.Exec(`INSERT INTO panel_settings(key,value) VALUES('ban_policy','{bad json')`); err != nil {
		t.Fatal(err)
	}
	p := app.readBanPolicy()
	if p.Threshold != 20 || p.WindowMin != 5 {
		t.Fatalf("expected defaults after invalid JSON, got %+v", p)
	}
}

func TestReadBanPolicyClampsNonPositiveValues(t *testing.T) {
	app, db := newTestApp(t)
	defer db.Close()
	if _, err := db.Exec(`INSERT INTO panel_settings(key,value) VALUES('ban_policy','{"enabled":true,"threshold":0,"window_min":0,"ban_min":0}')`); err != nil {
		t.Fatal(err)
	}
	p := app.readBanPolicy()
	if p.Threshold != 20 || p.WindowMin != 5 || p.BanMin != 15 {
		t.Fatalf("expected clamped defaults, got %+v", p)
	}
}

func TestAuthorizeSetupLoopbackAllowedWithoutToken(t *testing.T) {
	t.Setenv("VIYA_SETUP_REMOTE", "false")
	app := &App{setupToken: "secret"}
	r := httptest.NewRequest("POST", "/api/v1/auth/setup", nil)
	r.RemoteAddr = "127.0.0.1:12345"
	if !app.authorizeSetup(r) {
		t.Fatal("loopback should be allowed without a token")
	}
}

func TestAuthorizeSetupRemoteRequiresToken(t *testing.T) {
	t.Setenv("VIYA_SETUP_REMOTE", "true")
	app := &App{setupToken: "secret", dataDir: t.TempDir()}

	r := httptest.NewRequest("POST", "/api/v1/auth/setup", nil)
	r.RemoteAddr = "203.0.113.5:12345"
	if app.authorizeSetup(r) {
		t.Fatal("remote without token must be rejected")
	}

	r2 := httptest.NewRequest("POST", "/api/v1/auth/setup", nil)
	r2.RemoteAddr = "203.0.113.5:12345"
	r2.Header.Set("X-Viya-Setup-Token", "wrong")
	if app.authorizeSetup(r2) {
		t.Fatal("wrong token must be rejected")
	}

	r3 := httptest.NewRequest("POST", "/api/v1/auth/setup", nil)
	r3.RemoteAddr = "203.0.113.5:12345"
	r3.Header.Set("X-Viya-Setup-Token", "secret")
	if !app.authorizeSetup(r3) {
		t.Fatal("correct token must be accepted")
	}
	if app.setupToken != "" {
		t.Fatal("token must be burned after use")
	}

	r4 := httptest.NewRequest("POST", "/api/v1/auth/setup", nil)
	r4.RemoteAddr = "203.0.113.5:12345"
	r4.Header.Set("X-Viya-Setup-Token", "secret")
	if app.authorizeSetup(r4) {
		t.Fatal("burned token must not be reusable")
	}
}

func TestAuthorizeSetupRemoteDisabledRejectsEvenWithToken(t *testing.T) {
	t.Setenv("VIYA_SETUP_REMOTE", "false")
	app := &App{setupToken: "secret"}
	r := httptest.NewRequest("POST", "/api/v1/auth/setup", nil)
	r.RemoteAddr = "203.0.113.5:12345"
	r.Header.Set("X-Viya-Setup-Token", "secret")
	if app.authorizeSetup(r) {
		t.Fatal("remote setup must be rejected when VIYA_SETUP_REMOTE is not true")
	}
}
