package main

import (
	"database/sql"
	"testing"
)

// Databases created by v0.6.7 or earlier still carry site_access_events. The
// upgrade has to retire it without touching the hourly statistics, and it has
// to leave the new aggregate table behind.
func TestUpgradeRetiresPerRequestLogTable(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if _, err = db.Exec(`PRAGMA foreign_keys=ON;
		CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY,name TEXT NOT NULL,applied_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP);
		INSERT INTO schema_migrations(version,name) VALUES(1,'legacy baseline'),(7,'configuration change audit');
		CREATE TABLE proxy_hosts(id INTEGER PRIMARY KEY,domain TEXT NOT NULL UNIQUE,upstream TEXT NOT NULL,enabled INTEGER NOT NULL DEFAULT 1,version INTEGER NOT NULL DEFAULT 1,created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP);
		CREATE TABLE traffic_hours(site_id INTEGER NOT NULL,hour INTEGER NOT NULL,requests INTEGER NOT NULL,errors INTEGER NOT NULL,upload INTEGER NOT NULL,download INTEGER NOT NULL,PRIMARY KEY(site_id,hour));
		CREATE TABLE site_access_events(id INTEGER PRIMARY KEY,site_id INTEGER,event TEXT);
		INSERT INTO proxy_hosts(id,domain,upstream) VALUES(1,'app.test','http://127.0.0.1:8080');
		INSERT INTO traffic_hours VALUES(1,1793318400,42,0,10,20);
		INSERT INTO site_access_events(site_id,event) VALUES(1,'{"request":{"uri":"/secret"}}');`); err != nil {
		t.Fatal(err)
	}
	if err = migrateSchema(db); err != nil {
		t.Fatal(err)
	}
	var version int
	if err = db.QueryRow(`SELECT MAX(version) FROM schema_migrations`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != 8 {
		t.Fatalf("schema version = %d, want 8", version)
	}
	var retired int
	if err = db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='site_access_events'`).Scan(&retired); err != nil {
		t.Fatal(err)
	}
	if retired != 0 {
		t.Fatal("site_access_events survived the upgrade")
	}
	var aggregates int
	if err = db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='site_error_hours'`).Scan(&aggregates); err != nil {
		t.Fatal(err)
	}
	if aggregates != 1 {
		t.Fatal("site_error_hours was not created")
	}
	var requests int
	if err = db.QueryRow(`SELECT SUM(requests) FROM traffic_hours`).Scan(&requests); err != nil {
		t.Fatal(err)
	}
	if requests != 42 {
		t.Fatalf("hourly statistics changed during migration: %d", requests)
	}
}
