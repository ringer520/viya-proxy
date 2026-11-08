package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func archiveTestApp(t *testing.T) *App {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	db.SetMaxOpenConns(1)
	if _, err = db.Exec(`CREATE TABLE dns_providers(id INTEGER PRIMARY KEY, name TEXT, credentials_ciphertext BLOB, credentials_nonce BLOB);
	CREATE TABLE custom_certificates(id INTEGER PRIMARY KEY);
	CREATE TABLE managed_certificates(id INTEGER PRIMARY KEY);
	CREATE TABLE proxy_hosts(id INTEGER PRIMARY KEY);
	CREATE TABLE layer4_rules(id INTEGER PRIMARY KEY);
	CREATE TABLE ddns_records(id INTEGER PRIMARY KEY);
	CREATE TABLE ip_bans(id INTEGER PRIMARY KEY);
	CREATE TABLE panel_settings(key TEXT PRIMARY KEY, value TEXT);
	CREATE TABLE monitors(id INTEGER PRIMARY KEY);
	CREATE TABLE config_revisions(id INTEGER PRIMARY KEY);
	INSERT INTO proxy_hosts(id) VALUES(1), (2);
	INSERT INTO panel_settings(key,value) VALUES('web_listeners','[]');`); err != nil {
		t.Fatal(err)
	}
	return &App{db: db, dataDir: t.TempDir(), masterKey: bytes.Repeat([]byte{7}, 32), certProgress: map[string]certificateProgress{}, ddnsRunning: map[int64]bool{}, failCounts: map[string][]time.Time{}}
}

func postJSON(t *testing.T, handler http.HandlerFunc, body any) *httptest.ResponseRecorder {
	t.Helper()
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	handler(recorder, httptest.NewRequest(http.MethodPost, "/api/v1/config/export", bytes.NewReader(payload)))
	return recorder
}

func TestExportRequiresPasswordAndEncryptsArchive(t *testing.T) {
	a := archiveTestApp(t)
	if recorder := postJSON(t, a.exportConfiguration, map[string]any{"preferences": map[string]string{}}); recorder.Code != http.StatusBadRequest {
		t.Fatalf("password-less export returned %d", recorder.Code)
	}
	recorder := postJSON(t, a.exportConfiguration, map[string]any{"preferences": map[string]string{"viya-theme": "dark"}, "password": testArchivePassword})
	if recorder.Code != http.StatusOK {
		t.Fatalf("export returned %d: %s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Header().Get("Content-Disposition"), "viya-config-") {
		t.Fatal("missing download file name")
	}
	body := recorder.Body.Bytes()
	for _, leak := range []string{"viya-theme", "web_listeners", `"proxy_hosts"`, `"payload"`} {
		if bytes.Contains(body, []byte(leak)) {
			t.Fatalf("exported archive leaks %q", leak)
		}
	}
	var envelope configEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatal(err)
	}
	if !envelope.Encrypted || envelope.Payload != nil || envelope.KDF != "scrypt" || len(envelope.Salt) != 16 {
		t.Fatal("exported archive is not encrypted as expected")
	}
	// The same archive must inspect with the right password and fail otherwise.
	if recorder = postJSON(t, a.inspectConfiguration, map[string]any{"archive": envelope, "password": testArchivePassword}); recorder.Code != http.StatusOK {
		t.Fatalf("inspect returned %d: %s", recorder.Code, recorder.Body.String())
	}
	var report map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if counts, ok := report["counts"].(map[string]any); !ok || counts["proxy_hosts"] != float64(2) {
		t.Fatalf("unexpected inspect report: %v", report["counts"])
	}
	if report["encrypted"] != true {
		t.Fatal("inspect did not report an encrypted archive")
	}
	if counts, ok := report["counts"].(map[string]any); !ok || counts["config_revisions"] != nil {
		t.Fatalf("configuration history leaked into the archive: %v", report["counts"])
	}
	for _, wrong := range []string{"", "wrong-password"} {
		if recorder = postJSON(t, a.inspectConfiguration, map[string]any{"archive": envelope, "password": wrong}); recorder.Code != http.StatusBadRequest {
			t.Fatalf("inspect with password %q returned %d", wrong, recorder.Code)
		}
	}
}

func TestArchiveCompatDropsLegacyRevisionTable(t *testing.T) {
	tables := []tableDump{
		{Name: "proxy_hosts"},
		{Name: "config_revisions"},
		{Name: "monitors"},
		{Name: "monitor_samples"},
	}
	active := activeArchiveTables(tables)
	if len(active) != 1 || active[0].Name != "proxy_hosts" {
		t.Fatalf("legacy configuration history was not filtered: %v", active)
	}
}
