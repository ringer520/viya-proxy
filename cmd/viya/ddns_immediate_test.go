package main

import (
	"context"
	"database/sql"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func newDDNSTestApp(t *testing.T) (*App, *sql.DB) {
	t.Helper()
	app, db := newTestApp(t)
	app.ctx = context.Background()
	app.masterKey = make([]byte, 32)
	app.ddnsRunning = make(map[int64]bool)
	_, err := db.Exec(`CREATE TABLE dns_providers(id INTEGER PRIMARY KEY, name TEXT NOT NULL, provider_type TEXT NOT NULL, credentials_ciphertext BLOB NOT NULL, credentials_nonce BLOB NOT NULL, key_version INTEGER NOT NULL DEFAULT 1, created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP, updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP);
CREATE TABLE ddns_records(id INTEGER PRIMARY KEY, name TEXT NOT NULL, comment TEXT NOT NULL DEFAULT 'viya', dns_provider_id INTEGER NOT NULL, zone TEXT NOT NULL, record_name TEXT NOT NULL, record_type TEXT NOT NULL, cname_target TEXT NOT NULL DEFAULT '', ttl_seconds INTEGER NOT NULL DEFAULT 300, interval_minutes INTEGER NOT NULL DEFAULT 5, enabled INTEGER NOT NULL DEFAULT 1, last_ip TEXT NOT NULL DEFAULT '', last_run_at INTEGER NOT NULL DEFAULT 0, next_run_at INTEGER NOT NULL DEFAULT 0, last_error TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP, updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP, ip_source TEXT NOT NULL DEFAULT 'http', interface_name TEXT NOT NULL DEFAULT '', interface_ip TEXT NOT NULL DEFAULT '', last_changed_at INTEGER NOT NULL DEFAULT 0, last_success_at INTEGER NOT NULL DEFAULT 0, last_status TEXT NOT NULL DEFAULT '', last_stage TEXT NOT NULL DEFAULT '');
CREATE TABLE ddns_runs(id INTEGER PRIMARY KEY, ddns_record_id INTEGER NOT NULL, observed_ip TEXT NOT NULL DEFAULT '', status TEXT NOT NULL, error_text TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP);
INSERT INTO dns_providers(id,name,provider_type,credentials_ciphertext,credentials_nonce,key_version) VALUES(1,'broken','cloudflare',X'00',X'00',1)`)
	if err != nil {
		t.Fatal(err)
	}
	return app, db
}

// A CNAME record with undecryptable provider credentials fails locally at the
// credentials stage, so these tests never touch the network. The point is only
// whether an attempt was started and persisted immediately after saving.
func saveDDNSRecord(t *testing.T, app *App, enabled string) *httptest.ResponseRecorder {
	t.Helper()
	body := `{"name":"test","dns_provider_id":1,"zone":"example.com","record_name":"home","record_type":"CNAME","cname_target":"target.example.com","ttl_seconds":300,"interval_minutes":5,"enabled":` + enabled + `}`
	req := httptest.NewRequest("POST", "/api/v1/ddns-records", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	app.createDDNSRecord(rec, req)
	return rec
}

func countDDNSRuns(db *sql.DB) int {
	var n int
	_ = db.QueryRow(`SELECT COUNT(*) FROM ddns_runs`).Scan(&n)
	return n
}

func TestSaveDDNSTriggersImmediateUpdate(t *testing.T) {
	app, db := newDDNSTestApp(t)
	defer db.Close()

	rec := saveDDNSRecord(t, app, "true")
	if rec.Code != 201 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}

	// No ticker is running in this test, so a run within 3 seconds can only
	// have come from the save handler itself.
	deadline := time.Now().Add(3 * time.Second)
	for countDDNSRuns(db) == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if countDDNSRuns(db) == 0 {
		t.Fatal("saving an enabled task did not start an immediate DDNS update")
	}

	var lastRun, nextRun int64
	if err := db.QueryRow(`SELECT last_run_at,next_run_at FROM ddns_records WHERE id=1`).Scan(&lastRun, &nextRun); err != nil {
		t.Fatal(err)
	}
	if lastRun == 0 {
		t.Fatal("last_run_at was not recorded")
	}
	if nextRun <= time.Now().Unix() {
		t.Fatal("next_run_at was not advanced, the minute ticker would run it twice")
	}
}

func TestDisabledDDNSDoesNotTriggerUpdate(t *testing.T) {
	app, db := newDDNSTestApp(t)
	defer db.Close()

	rec := saveDDNSRecord(t, app, "false")
	if rec.Code != 201 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	time.Sleep(300 * time.Millisecond)
	if n := countDDNSRuns(db); n != 0 {
		t.Fatalf("a disabled task must not publish DNS immediately, got %d runs", n)
	}
}

func TestRunDDNSOnceSkipsInFlightRecord(t *testing.T) {
	app, db := newDDNSTestApp(t)
	defer db.Close()
	if _, err := db.Exec(`INSERT INTO ddns_records(id,name,dns_provider_id,zone,record_name,record_type,cname_target,enabled) VALUES(1,'test',1,'example.com','home','CNAME','target.example.com',1)`); err != nil {
		t.Fatal(err)
	}
	app.ddnsRunning[1] = true
	app.runDDNSOnce(1)
	if n := countDDNSRuns(db); n != 0 {
		t.Fatalf("an in-flight record was executed a second time, got %d runs", n)
	}
}
