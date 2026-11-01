package main

import (
	"net/http/httptest"
	"testing"
)

func TestAllowLoginThrottlesAfterMaxAttempts(t *testing.T) {
	app, db := newTestApp(t)
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE login_attempts(ip TEXT PRIMARY KEY, window_start INTEGER NOT NULL DEFAULT 0, count INTEGER NOT NULL DEFAULT 0)`); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", "/api/v1/auth/login", nil)
	r.RemoteAddr = "203.0.113.9:12345"
	for i := 0; i < loginMaxAttempts; i++ {
		if !app.allowLogin(r) {
			t.Fatalf("attempt %d should be allowed", i+1)
		}
	}
	if app.allowLogin(r) {
		t.Fatalf("attempt %d should be throttled", loginMaxAttempts+1)
	}
}

func TestAllowLoginIsolationPerIP(t *testing.T) {
	app, db := newTestApp(t)
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE login_attempts(ip TEXT PRIMARY KEY, window_start INTEGER NOT NULL DEFAULT 0, count INTEGER NOT NULL DEFAULT 0)`); err != nil {
		t.Fatal(err)
	}
	a := httptest.NewRequest("POST", "/api/v1/auth/login", nil)
	a.RemoteAddr = "203.0.113.1:1"
	b := httptest.NewRequest("POST", "/api/v1/auth/login", nil)
	b.RemoteAddr = "203.0.113.2:1"
	for i := 0; i < loginMaxAttempts; i++ {
		app.allowLogin(a)
	}
	// A different IP is unaffected by the first IP hitting its limit.
	if !app.allowLogin(b) {
		t.Fatal("a different IP should not share the throttle bucket")
	}
}
