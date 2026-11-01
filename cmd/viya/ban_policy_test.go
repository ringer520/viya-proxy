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

func TestAuthorizeSetupAllowsAnyAddress(t *testing.T) {
	app := &App{}
	for _, address := range []string{"127.0.0.1:12345", "192.168.1.10:12345", "203.0.113.5:12345", "[2001:db8::5]:12345"} {
		r := httptest.NewRequest("POST", "/api/v1/auth/setup", nil)
		r.RemoteAddr = address
		if !app.authorizeSetup(r) {
			t.Fatalf("setup should be allowed from %s", address)
		}
	}
}
