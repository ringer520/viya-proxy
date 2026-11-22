package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestApplyFailureRestoresPreviousRuntime(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses POSIX fake validator; exercised by Linux CI")
	}
	a := stabilityApp(t)
	a.httpListen = ":18080"
	a.adminListen = "127.0.0.1:2019"
	if err := a.initListenerSites(); err != nil {
		t.Fatal(err)
	}
	if err := a.initCertificateRequests(); err != nil {
		t.Fatal(err)
	}
	a.caddyBin = filepath.Join(t.TempDir(), "validator")
	os.WriteFile(a.caddyBin, []byte("#!/bin/sh\nexit 0\n"), 0700)
	old := []byte(`{"apps":{}}`)
	loads := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			w.Write(old)
			return
		}
		loads++
		if loads == 1 {
			w.WriteHeader(500)
			return
		}
		var body map[string]any
		if json.NewDecoder(r.Body).Decode(&body) != nil || configurationHash(mustJSON(body)) != configurationHash(old) {
			t.Error("recovery did not use exact previous runtime")
		}
		w.WriteHeader(200)
	}))
	defer server.Close()
	a.adminURL = server.URL
	a.journalApplication(old, true)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if a.apply(ctx) == nil {
		t.Fatal("engine failure reported as success")
	}
	if loads != 2 || !a.applyState.Pending || a.applyState.Stage != "failed" {
		t.Fatalf("invalid recovery: loads=%d state=%+v", loads, a.applyState)
	}
}
func mustJSON(value any) []byte { body, _ := json.Marshal(value); return body }

func TestStaticCleanupPreservesReusedWebsite(t *testing.T) {
	a := stabilityApp(t)
	root := filepath.Join(a.dataDir, "sites", "old.test")
	os.MkdirAll(root, 0700)
	file := filepath.Join(root, "index.html")
	os.WriteFile(file, []byte("keep"), 0600)
	a.db.Exec(`INSERT INTO pending_file_deletions VALUES(?)`, root)
	a.db.Exec(`INSERT INTO proxy_hosts(domain,upstream,type,root_path) VALUES('new.test','','static',?)`, root)
	if err := a.cleanupDeletedSites(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(file); err != nil {
		t.Fatal("reused website files removed")
	}
	a.db.Exec(`DELETE FROM proxy_hosts`)
	a.db.Exec(`INSERT INTO pending_file_deletions VALUES(?)`, root)
	if err := a.cleanupDeletedSites(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatal("orphan cleanup did not run")
	}
}

func stabilityApp(t *testing.T) *App {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	if err := migrateSchema(db); err != nil {
		t.Fatal(err)
	}
	a := &App{db: db, ctx: context.Background(), dataDir: t.TempDir()}
	if err := ensureColumn(db, "ip_bans", "source", "TEXT NOT NULL DEFAULT 'auto'"); err != nil {
		t.Fatal(err)
	}
	if err := a.initTraffic(); err != nil {
		t.Fatal(err)
	}
	return a
}
func TestVersionedMigrationResumeAndFutureGuard(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	defer db.Close()
	if err := migrateBaselineSchema(db); err != nil {
		t.Fatal(err)
	}
	a := &App{db: db}
	if _, err := a.db.Exec(`INSERT INTO users(username,password_hash) VALUES('existing',x'01'); CREATE TABLE IF NOT EXISTS monitors(id INTEGER PRIMARY KEY); CREATE TABLE IF NOT EXISTS monitor_samples(id INTEGER PRIMARY KEY);`); err != nil {
		t.Fatal(err)
	}
	if err := migrateSchema(a.db); err != nil {
		t.Fatal(err)
	}
	if err := migrateSchema(a.db); err != nil {
		t.Fatal(err)
	}
	var n int
	a.db.QueryRow(`SELECT COUNT(*) FROM users WHERE username='existing'`).Scan(&n)
	if n != 1 {
		t.Fatal("migration lost account")
	}
	a.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name IN ('monitors','monitor_samples')`).Scan(&n)
	if n != 0 {
		t.Fatal("retired monitor tables remain")
	}
	a.db.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&n)
	if n != schemaVersion {
		t.Fatalf("migration ledger: %d", n)
	}
	a.db.Exec(`INSERT INTO schema_migrations(version,name) VALUES(999,'future')`)
	if migrateSchema(a.db) == nil {
		t.Fatal("older binary accepted future schema")
	}
}
func TestApplyJournalSurvivesRestartAndStages(t *testing.T) {
	a := stabilityApp(t)
	a.setApplyStage("applied", "ok")
	a.journalApplication([]byte(`{"a":1,"b":2}`), true)
	a.setApplyStage("failed", "failed")
	a.journalApplication([]byte(`{"a":3}`), false)
	restarted := &App{db: a.db}
	restarted.loadApplicationJournal()
	if !restarted.applyState.Pending || restarted.applyState.Stage != "failed" {
		t.Fatalf("lost pending status: %+v", restarted.applyState)
	}
	restarted.setApplyStage("validating", "checking")
	if restarted.applyState.AppliedHash == "" {
		t.Fatal("stage transition lost applied identity")
	}
	if configurationHash([]byte(`{ "b": 2, "a": 1 }`)) != configurationHash([]byte(`{"a":1,"b":2}`)) {
		t.Fatal("formatting changed config identity")
	}
}
func TestRetentionPrunesOnlyExpiredStatistics(t *testing.T) {
	a := stabilityApp(t)
	_, err := a.db.Exec(`INSERT INTO proxy_hosts(id,domain,upstream) VALUES(1,'one.test','http://localhost'),(2,'two.test','http://localhost'); INSERT INTO panel_settings VALUES('retention','{"traffic_days":30}');`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = a.db.Exec(`INSERT INTO traffic_hours VALUES(1,?,1,0,0,0),(1,?,1,0,0,0)`, time.Now().Add(-60*24*time.Hour).Unix(), time.Now().Unix())
	if err != nil {
		t.Fatal(err)
	}
	_, err = a.db.Exec(`INSERT INTO site_error_hours VALUES(1,?,502,3),(1,?,404,1)`, time.Now().Add(-60*24*time.Hour).Unix()/3600*3600, time.Now().Unix()/3600*3600)
	if err != nil {
		t.Fatal(err)
	}
	if err = a.pruneTraffic(context.Background()); err != nil {
		t.Fatal(err)
	}
	var n int
	a.db.QueryRow(`SELECT COUNT(*) FROM traffic_hours`).Scan(&n)
	if n != 1 {
		t.Fatal("retention did not prune old hours")
	}
	a.db.QueryRow(`SELECT COUNT(*) FROM site_error_hours`).Scan(&n)
	if n != 1 {
		t.Fatal("retention did not prune old error buckets")
	}
	a.db.QueryRow(`SELECT COUNT(*) FROM proxy_hosts`).Scan(&n)
	if n != 2 {
		t.Fatal("cleanup removed sites")
	}
	for _, v := range []retentionSettings{{29}, {366}, {0}} {
		if validRetention(v) {
			t.Fatal("accepted unsafe retention", v)
		}
	}
}
func TestStorageUsageCategoriesAndCleanupConfirmation(t *testing.T) {
	a := stabilityApp(t)
	for _, name := range []string{"sites/one/index.html", "custom-certs/a.pem", "program.log", "recovery/current.json", "viya.db"} {
		p := filepath.Join(a.dataDir, filepath.FromSlash(name))
		os.MkdirAll(filepath.Dir(p), 0700)
		os.WriteFile(p, []byte("abc"), 0600)
	}
	w := httptest.NewRecorder()
	a.storageUsage(w, httptest.NewRequest("GET", "/api/v1/storage", nil))
	var result struct {
		Total      int64             `json:"total"`
		Categories []storageCategory `json:"categories"`
	}
	if json.Unmarshal(w.Body.Bytes(), &result) != nil || result.Total != 15 {
		t.Fatal(w.Body.String())
	}
	w = httptest.NewRecorder()
	a.cleanupStorage(w, httptest.NewRequest("POST", "/api/v1/storage/cleanup", strings.NewReader(`{"confirmed":false}`)))
	if w.Code != 400 {
		t.Fatal("unconfirmed cleanup accepted")
	}
}
func TestLegacyArchiveAdapterKeepsUnknownTablesForRejection(t *testing.T) {
	tables := activeArchiveTables([]tableDump{{Name: "monitors"}, {Name: "proxy_hosts"}, {Name: "unknown"}, {Name: "monitor_samples"}})
	if len(tables) != 2 || tables[0].Name != "proxy_hosts" || tables[1].Name != "unknown" {
		t.Fatalf("invalid compatibility filtering: %+v", tables)
	}
}

func TestValidateRestoredBindingsAcceptsFilesSite(t *testing.T) {
	a := stabilityApp(t)
	if err := a.initListenerSites(); err != nil {
		t.Fatal(err)
	}
	if err := a.initCertificateRequests(); err != nil {
		t.Fatal(err)
	}
	shareDir := filepath.Join(a.dataDir, "sites", "files.test")
	if err := os.MkdirAll(shareDir, 0o750); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec(
		`INSERT INTO proxy_hosts(name,domain,listener_id,upstream,enabled,version,tls_mode,type,root_path,basic_auth_user,basic_auth_hash,browse,webdav,webdav_write) VALUES('files','files.test','http-main','',1,1,'none','files',?,'u','hash',1,1,1)`,
		shareDir,
	); err != nil {
		t.Fatal(err)
	}
	if err := a.validateRestoredBindings(context.Background()); err != nil {
		t.Fatalf("validateRestoredBindings failed on valid files site: %v", err)
	}
}

func TestRemoveStoredCertificatesWildcardPreservesApex(t *testing.T) {
	certRoot := filepath.Join(t.TempDir(), "certificates")
	apexDir := filepath.Join(certRoot, "acme-v02", "example.com")
	wildDir := filepath.Join(certRoot, "acme-v02", "wildcard_.example.com")
	if err := os.MkdirAll(apexDir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(wildDir, 0o750); err != nil {
		t.Fatal(err)
	}
	removed, err := removeStoredCertificates(certRoot, "*.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if removed != 1 {
		t.Fatalf("removed = %d, want 1", removed)
	}
	if _, statErr := os.Stat(wildDir); !os.IsNotExist(statErr) {
		t.Fatal("wildcard directory should be removed")
	}
	if _, statErr := os.Stat(apexDir); statErr != nil {
		t.Fatal("apex directory must be preserved when removing wildcard cert")
	}
}

func TestSiteAccessLogsMissingFileAndWildcardHost(t *testing.T) {
	a := stabilityApp(t)
	if _, err := a.db.Exec(`INSERT INTO proxy_hosts(id,domain,upstream) VALUES(1,'*.example.com','http://127.0.0.1:8080')`); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/proxy-hosts/1/logs", nil)
	req.SetPathValue("id", "1")
	rec := httptest.NewRecorder()
	a.siteAccessLogs(rec, req)
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Fatalf("missing access.log status=%d body=%q, want 200 []", rec.Code, rec.Body.String())
	}

	logContent := `{"logger":"http.log.access","ts":1,"request":{"host":"api.example.com","uri":"/v1"},"status":200}` + "\n" +
		`{"logger":"http.log.access","ts":2,"request":{"host":"other.test","uri":"/"},"status":200}` + "\n"
	if err := os.WriteFile(filepath.Join(a.dataDir, "access.log"), []byte(logContent), 0o600); err != nil {
		t.Fatal(err)
	}
	rec2 := httptest.NewRecorder()
	a.siteAccessLogs(rec2, req)
	if rec2.Code != http.StatusOK {
		t.Fatalf("wildcard siteAccessLogs status = %d", rec2.Code)
	}
	var events []map[string]any
	if err := json.Unmarshal(rec2.Body.Bytes(), &events); err != nil || len(events) != 1 {
		t.Fatalf("wildcard events = %v (err=%v), want 1", events, err)
	}
}
