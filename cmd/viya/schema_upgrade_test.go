package main

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

// An install created before the routing / file-service release sits at schema
// 5 and has none of the new proxy_hosts columns. It never re-runs the baseline,
// so startup must top those columns up on its own — otherwise every
// proxy-hosts query fails with "no such column".
func TestUpgradeAddsRoutingAndFileServiceColumns(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "legacy.db"))
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	defer db.Close()

	if _, err := db.Exec(`
		CREATE TABLE proxy_hosts(id INTEGER PRIMARY KEY, domain TEXT NOT NULL UNIQUE, upstream TEXT NOT NULL,
			enabled INTEGER NOT NULL DEFAULT 1, version INTEGER NOT NULL DEFAULT 1,
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP, updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP);
		INSERT INTO proxy_hosts(domain,upstream) VALUES('legacy.local','http://127.0.0.1:9000');
		CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY, name TEXT NOT NULL, applied_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP);
		INSERT INTO schema_migrations(version,name) VALUES(1,'legacy baseline'),(2,'monitors'),(3,'journal'),(4,'deletions'),(5,'status');`); err != nil {
		t.Fatal(err)
	}

	if err := migrateSchema(db); err != nil {
		t.Fatalf("upgrade migration failed: %v", err)
	}

	present := map[string]bool{}
	rows, err := db.Query(`PRAGMA table_info(proxy_hosts)`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, dataType string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &primaryKey); err != nil {
			t.Fatal(err)
		}
		present[name] = true
	}
	rows.Close()
	for _, column := range proxyHostAdditiveColumns {
		if !present[column.name] {
			t.Fatalf("column %s was not added on upgrade", column.name)
		}
	}

	var domain string
	var stripPrefix, webdav int
	if err := db.QueryRow(`SELECT domain, strip_prefix, webdav FROM proxy_hosts WHERE id=1`).Scan(&domain, &stripPrefix, &webdav); err != nil {
		t.Fatalf("existing row unreadable after upgrade: %v", err)
	}
	if domain != "legacy.local" || stripPrefix != 0 || webdav != 0 {
		t.Fatalf("existing row changed: %q %d %d", domain, stripPrefix, webdav)
	}

	// Restarting must stay a no-op instead of failing on duplicate columns.
	if err := migrateSchema(db); err != nil {
		t.Fatalf("second startup failed: %v", err)
	}
}
