package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func auditContext() context.Context {
	ctx := context.WithValue(context.Background(), requestIDKey{}, "req-audit-test")
	return context.WithValue(ctx, operationContextKey{}, operationContext{Summary: "添加 Web 站点", Actor: "admin"})
}

func TestConfigAuditRecordsOnlySuccessfulMutations(t *testing.T) {
	a := stabilityApp(t)
	ctx := auditContext()
	a.recordConfigAudit(ctx, "admin", "POST", "/api/v1/proxy-hosts", 200)
	a.recordConfigAudit(ctx, "admin", "POST", "/api/v1/proxy-hosts", 400)
	a.recordConfigAudit(ctx, "admin", "GET", "/api/v1/proxy-hosts", 200)
	a.recordConfigAudit(ctx, "admin", "POST", "/api/v1/auth/logout", 200)
	var count int
	if err := a.db.QueryRow(`SELECT COUNT(*) FROM config_audit`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("recorded %d audit rows, want exactly one successful mutation", count)
	}
	var e configAuditEntry
	if err := a.db.QueryRow(`SELECT id,username,method,path,summary,status,request_id,created_at FROM config_audit LIMIT 1`).
		Scan(&e.ID, &e.Username, &e.Method, &e.Path, &e.Summary, &e.Status, &e.RequestID, &e.CreatedAt); err != nil {
		t.Fatal(err)
	}
	if e.Username != "admin" || e.Method != "POST" || e.Summary != "添加 Web 站点" || e.RequestID != "req-audit-test" || e.CreatedAt == 0 {
		t.Fatalf("unexpected audit row: %+v", e)
	}
}

func TestConfigAuditIsNotExported(t *testing.T) {
	for _, name := range append(append([]string{}, configTables...), runtimeTables...) {
		if name == "config_audit" {
			t.Fatal("config_audit must stay out of configuration exports and recovery archives")
		}
	}
}

func TestListConfigAuditFiltersByText(t *testing.T) {
	a := stabilityApp(t)
	ctx := auditContext()
	a.recordConfigAudit(ctx, "admin", "DELETE", "/api/v1/layer4-rules/9", 200)
	rec := httptest.NewRecorder()
	a.listConfigAudit(rec, httptest.NewRequest("GET", "/api/v1/config-audit?q=Web", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("list returned %d: %s", rec.Code, rec.Body.String())
	}
	var entries []configAuditEntry
	if err := json.Unmarshal(rec.Body.Bytes(), &entries); err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Path != "/api/v1/layer4-rules/9" {
		t.Fatalf("unexpected list result: %+v", entries)
	}
	rec = httptest.NewRecorder()
	a.listConfigAudit(rec, httptest.NewRequest("GET", "/api/v1/config-audit?q=nonexistent", nil))
	if err := json.Unmarshal(rec.Body.Bytes(), &entries); err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("text filter did not exclude unrelated rows: %+v", entries)
	}
}
