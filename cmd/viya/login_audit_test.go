package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func newAuditApp(t *testing.T) (*App, func()) {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	if _, err = db.Exec(`CREATE TABLE login_audit(id INTEGER PRIMARY KEY, username TEXT NOT NULL DEFAULT '', ip TEXT NOT NULL DEFAULT '', user_agent TEXT NOT NULL DEFAULT '', result TEXT NOT NULL, reason TEXT NOT NULL DEFAULT '', created_at INTEGER NOT NULL DEFAULT 0)`); err != nil {
		db.Close()
		t.Fatal(err)
	}
	return &App{db: db, ctx: context.Background()}, func() { db.Close() }
}

func TestLoginAuditRecordsAndOrdersNewestFirst(t *testing.T) {
	a, done := newAuditApp(t)
	defer done()

	failure := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{}`))
	failure.RemoteAddr = "192.0.2.10:51000"
	failure.Header.Set("User-Agent", "Mozilla/5.0 test-agent")
	a.recordLoginAudit(failure, "  admin  ", auditFailure, auditReasonPassword)

	success := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{}`))
	success.RemoteAddr = "192.0.2.11:51001"
	a.recordLoginAudit(success, "admin", auditSuccess, auditReasonPassword)

	recorder := httptest.NewRecorder()
	a.listLoginAudit(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/auth/audit", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var entries []loginAuditEntry
	if err := json.Unmarshal(recorder.Body.Bytes(), &entries); err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(entries))
	}
	if entries[0].Result != auditSuccess || entries[1].Result != auditFailure {
		t.Fatalf("ordering = %s,%s; want newest first", entries[0].Result, entries[1].Result)
	}
	if entries[1].Username != "admin" {
		t.Fatalf("username = %q, want trimmed", entries[1].Username)
	}
	if entries[1].IP != "192.0.2.10" || entries[0].IP != "192.0.2.11" {
		t.Fatalf("ips = %q,%q", entries[0].IP, entries[1].IP)
	}
	if entries[1].UserAgent != "Mozilla/5.0 test-agent" {
		t.Fatalf("user agent = %q", entries[1].UserAgent)
	}
	if entries[0].CreatedAt == 0 || entries[0].CreatedAt > time.Now().Unix()+5 {
		t.Fatalf("created_at = %d", entries[0].CreatedAt)
	}
}

func TestLoginAuditClipsOversizedFields(t *testing.T) {
	a, done := newAuditApp(t)
	defer done()

	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{}`))
	request.RemoteAddr = "198.51.100.7:40000"
	request.Header.Set("User-Agent", strings.Repeat("x", loginAuditAgent+50))
	a.recordLoginAudit(request, strings.Repeat("u", loginAuditUsername+50), auditFailure, auditReasonUnknownUser)

	var username, agent string
	if err := a.db.QueryRow(`SELECT username,user_agent FROM login_audit`).Scan(&username, &agent); err != nil {
		t.Fatal(err)
	}
	if len(username) != loginAuditUsername || len(agent) != loginAuditAgent {
		t.Fatalf("username = %d bytes, agent = %d bytes", len(username), len(agent))
	}
	// Multi-byte input must be cut on a rune boundary, never mid-character.
	// A 7-byte budget cannot hold 中文名 (9 bytes), so it must fall back to
	// 中文 rather than emit a half rune.
	if got := clipText("中文名称超长截断测试", 7); got != "中文" {
		t.Fatalf("clipText = %q, want %q", got, "中文")
	}
	if got := clipText("short", 32); got != "short" {
		t.Fatalf("clipText padded or changed short input: %q", got)
	}
}

func TestLoginAuditLimitQueryIsBounded(t *testing.T) {
	a, done := newAuditApp(t)
	defer done()

	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{}`))
	for i := 0; i < 5; i++ {
		a.recordLoginAudit(request, "admin", auditSuccess, auditReasonPassword)
	}
	for _, test := range []struct {
		query string
		want  int
	}{
		{"", 5},
		{"?limit=2", 2},
		{"?limit=0", 5},
		{"?limit=abc", 5},
		{"?limit=100000", 5},
	} {
		recorder := httptest.NewRecorder()
		a.listLoginAudit(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/auth/audit"+test.query, nil))
		var entries []loginAuditEntry
		if err := json.Unmarshal(recorder.Body.Bytes(), &entries); err != nil {
			t.Fatal(err)
		}
		if len(entries) != test.want {
			t.Fatalf("%q returned %d entries, want %d", test.query, len(entries), test.want)
		}
	}
}
