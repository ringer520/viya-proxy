package main

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
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

func TestAllowLoginRespectsTrustedProxy(t *testing.T) {
	app, db := newTestApp(t)
	defer db.Close()
	proxies, err := parseTrustedProxies("127.0.0.1,::1")
	if err != nil {
		t.Fatal(err)
	}
	app.trustedProxies = proxies
	if _, err := db.Exec(`CREATE TABLE login_attempts(ip TEXT PRIMARY KEY, window_start INTEGER NOT NULL DEFAULT 0, count INTEGER NOT NULL DEFAULT 0)`); err != nil {
		t.Fatal(err)
	}

	clientA := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
	clientA.RemoteAddr = "127.0.0.1:41000"
	clientA.Header.Set("X-Forwarded-For", "198.51.100.10")

	clientB := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
	clientB.RemoteAddr = "127.0.0.1:41001"
	clientB.Header.Set("X-Forwarded-For", "198.51.100.11")

	for i := 0; i < loginMaxAttempts; i++ {
		if !app.allowLogin(clientA) {
			t.Fatalf("client A attempt %d should be allowed", i+1)
		}
	}
	if app.allowLogin(clientA) {
		t.Fatal("client A should be throttled after max attempts")
	}
	if !app.allowLogin(clientB) {
		t.Fatal("client B behind the same trusted proxy must not share client A's bucket")
	}

	// Untrusted proxy cannot spoof X-Forwarded-For to bypass its own bucket.
	spoofed := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
	spoofed.RemoteAddr = "203.0.113.50:5555"
	for i := 0; i < loginMaxAttempts; i++ {
		spoofed.Header.Set("X-Forwarded-For", "198.51.100."+string(rune('1'+i)))
		if !app.allowLogin(spoofed) {
			t.Fatalf("untrusted attempt %d should be allowed before limit", i+1)
		}
	}
	spoofed.Header.Set("X-Forwarded-For", "198.51.100.250")
	if app.allowLogin(spoofed) {
		t.Fatal("untrusted peer must be throttled by its real socket IP")
	}
}

func TestCookieSecureMatchesRequestScheme(t *testing.T) {
	app, db := newTestApp(t)
	defer db.Close()
	proxies, err := parseTrustedProxies("127.0.0.1,::1")
	if err != nil {
		t.Fatal(err)
	}
	app.trustedProxies = proxies
	app.secureCookies = true

	if _, err := db.Exec(`CREATE TABLE users(id INTEGER PRIMARY KEY, username TEXT NOT NULL UNIQUE, password_hash TEXT NOT NULL, role TEXT NOT NULL DEFAULT 'admin');
		CREATE TABLE sessions(token_hash BLOB PRIMARY KEY, user_id INTEGER NOT NULL, csrf_token TEXT NOT NULL, expires_at INTEGER NOT NULL);`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO users(id,username,password_hash) VALUES(1,'admin','hash')`); err != nil {
		t.Fatal(err)
	}

	plainHTTP := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
	plainHTTP.RemoteAddr = "203.0.113.5:1234"
	plainHTTP.Header.Set("X-Forwarded-Proto", "https") // untrusted peer cannot spoof HTTPS
	if app.cookieSecure(plainHTTP) {
		t.Fatal("untrusted plain HTTP request must not produce Secure cookie")
	}

	rec := httptest.NewRecorder()
	if err := app.createSessionForUser(rec, plainHTTP, 1); err != nil {
		t.Fatal(err)
	}
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Secure {
		t.Fatalf("expected non-Secure cookie on HTTP, got %+v", cookies)
	}

	renewRec := httptest.NewRecorder()
	app.renewSession(renewRec, plainHTTP, cookies[0].Value, time.Now().Add(time.Minute).Unix())
	renewed := renewRec.Result().Cookies()
	if len(renewed) != 1 || renewed[0].Secure {
		t.Fatalf("renewed cookie on HTTP must remain non-Secure, got %+v", renewed)
	}

	proxiedHTTPS := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
	proxiedHTTPS.RemoteAddr = "127.0.0.1:40000"
	proxiedHTTPS.Header.Set("X-Forwarded-Proto", "https")
	if !app.cookieSecure(proxiedHTTPS) {
		t.Fatal("trusted proxy with X-Forwarded-Proto: https must set Secure cookie")
	}

	directTLS := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
	directTLS.TLS = &tls.ConnectionState{}
	if !app.cookieSecure(directTLS) {
		t.Fatal("direct TLS request must set Secure cookie")
	}
}

func TestRequestSchemeAndAccessClientIPMultiHeader(t *testing.T) {
	_, net127, _ := net.ParseCIDR("127.0.0.0/8")
	a := &App{trustedProxies: []*net.IPNet{net127}}

	req := httptest.NewRequest(http.MethodGet, "http://panel.test/", nil)
	req.RemoteAddr = "127.0.0.1:41000"
	req.Header.Add("X-Forwarded-Proto", "http")
	req.Header.Add("X-Forwarded-Proto", "HTTPS")
	if got := requestScheme(req, a.trustedProxies); got != "https" {
		t.Fatalf("requestScheme = %q, want https", got)
	}

	req.Header.Add("X-Forwarded-For", "127.0.0.1")
	req.Header.Add("X-Forwarded-For", "198.51.100.77")
	if got := a.accessClientIP(req); got != "198.51.100.77" {
		t.Fatalf("accessClientIP = %q, want 198.51.100.77", got)
	}
}

func TestChangePasswordWrongCurrentReturns400AndUpdateUserPromotesViewer(t *testing.T) {
	a := stabilityApp(t)
	hash, err := bcrypt.GenerateFromPassword([]byte("SecretPass123"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec(`INSERT INTO users(id,username,password_hash,role) VALUES(1,'admin',?,'admin'),(2,'reader',?,'viewer')`, hash, hash); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/password", strings.NewReader(`{"current_password":"wrong-pass","new_password":"NewPassword123","confirm_password":"NewPassword123"}`))
	req = req.WithContext(context.WithValue(req.Context(), sessionContextKey{}, sessionContext{username: "admin", role: roleAdmin}))
	rec := httptest.NewRecorder()
	a.changeAdminPassword(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("changeAdminPassword wrong current status = %d, want 400: %s", rec.Code, rec.Body.String())
	}

	promoteReq := httptest.NewRequest(http.MethodPut, "/api/v1/users/2", strings.NewReader(`{"role":"admin"}`))
	promoteReq.SetPathValue("id", "2")
	promoteReq = promoteReq.WithContext(context.WithValue(promoteReq.Context(), sessionContextKey{}, sessionContext{username: "admin", role: roleAdmin}))
	promoteRec := httptest.NewRecorder()
	a.updateUser(promoteRec, promoteReq)
	if promoteRec.Code != http.StatusOK {
		t.Fatalf("updateUser promote viewer status = %d, want 200: %s", promoteRec.Code, promoteRec.Body.String())
	}

	missingReq := httptest.NewRequest(http.MethodPut, "/api/v1/users/999", strings.NewReader(`{"role":"viewer"}`))
	missingReq.SetPathValue("id", "999")
	missingReq = missingReq.WithContext(context.WithValue(missingReq.Context(), sessionContextKey{}, sessionContext{username: "admin", role: roleAdmin}))
	missingRec := httptest.NewRecorder()
	a.updateUser(missingRec, missingReq)
	if missingRec.Code != http.StatusNotFound {
		t.Fatalf("updateUser missing user status = %d, want 404", missingRec.Code)
	}
}
