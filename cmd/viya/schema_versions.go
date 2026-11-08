package main

import (
	"database/sql"
	"fmt"
)

const schemaVersion = 6

// The baseline is idempotent so interrupted upgrades can safely resume. New
// migrations are transactional and are recorded only after they complete.
func migrateSchema(db *sql.DB) error {
	if _, err := db.Exec(`PRAGMA foreign_keys=ON; PRAGMA busy_timeout=5000; CREATE TABLE IF NOT EXISTS schema_migrations(version INTEGER PRIMARY KEY, name TEXT NOT NULL, applied_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP)`); err != nil {
		return err
	}
	var current int
	if err := db.QueryRow(`SELECT COALESCE(MAX(version),0) FROM schema_migrations`).Scan(&current); err != nil {
		return err
	}
	if current > schemaVersion {
		return fmt.Errorf("database schema %d is newer than supported schema %d; use a matching binary or restore a backup", current, schemaVersion)
	}
	if current < 1 {
		if err := migrateBaselineSchema(db); err != nil {
			return err
		}
		if _, err := db.Exec(`INSERT INTO schema_migrations(version,name) VALUES(1,'legacy baseline')`); err != nil {
			return err
		}
	}
	migrations := []struct {
		version   int
		name, sql string
	}{
		{2, "retire obsolete monitor storage", `DROP TABLE IF EXISTS monitor_samples; DROP TABLE IF EXISTS monitors; UPDATE proxy_hosts SET spa_fallback=0,browse=0; CREATE INDEX IF NOT EXISTS idx_sessions_expiry ON sessions(expires_at); CREATE INDEX IF NOT EXISTS idx_login_audit_created ON login_audit(created_at);`},
		{3, "configuration application journal", `CREATE TABLE IF NOT EXISTS config_application(id INTEGER PRIMARY KEY CHECK(id=1),desired_hash TEXT NOT NULL DEFAULT '',applied_hash TEXT NOT NULL DEFAULT '',stage TEXT NOT NULL DEFAULT '',updated TEXT NOT NULL DEFAULT '');`},
		{4, "defer static file deletion until engine apply", `CREATE TABLE IF NOT EXISTS pending_file_deletions(root_path TEXT PRIMARY KEY);`},
		{5, "operational status and access recovery", `ALTER TABLE ddns_records ADD COLUMN last_changed_at INTEGER NOT NULL DEFAULT 0;
ALTER TABLE ddns_records ADD COLUMN last_success_at INTEGER NOT NULL DEFAULT 0;
ALTER TABLE ddns_records ADD COLUMN last_status TEXT NOT NULL DEFAULT '';
ALTER TABLE ddns_records ADD COLUMN last_stage TEXT NOT NULL DEFAULT '';
UPDATE ddns_records SET last_changed_at=COALESCE((SELECT MAX(CAST(strftime('%s',created_at) AS INTEGER)) FROM ddns_runs WHERE ddns_record_id=ddns_records.id AND status='updated'),0),last_success_at=COALESCE((SELECT MAX(CAST(strftime('%s',created_at) AS INTEGER)) FROM ddns_runs WHERE ddns_record_id=ddns_records.id AND status IN ('updated','unchanged')),0),last_status=COALESCE((SELECT status FROM ddns_runs WHERE ddns_record_id=ddns_records.id ORDER BY id DESC LIMIT 1),'');
CREATE TABLE access_rule_guards(id INTEGER PRIMARY KEY CHECK(id=1),ban_id INTEGER NOT NULL,before_json TEXT NOT NULL,deadline INTEGER NOT NULL,domain TEXT NOT NULL);
CREATE TABLE provider_checks(provider_id INTEGER PRIMARY KEY REFERENCES dns_providers(id) ON DELETE CASCADE,status TEXT NOT NULL,message TEXT NOT NULL,checked_at INTEGER NOT NULL);`},
		{6, "retire configuration history", `DROP TABLE IF EXISTS config_revisions;`},
	}
	for _, m := range migrations {
		if m.version <= current {
			continue
		}
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		if _, err = tx.Exec(m.sql); err == nil {
			_, err = tx.Exec(`INSERT INTO schema_migrations(version,name) VALUES(?,?)`, m.version, m.name)
		}
		if err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %d (%s): %w", m.version, m.name, err)
		}
		if err = tx.Commit(); err != nil {
			return err
		}
	}
	// Additive columns must also land on databases that were created by an
	// older release, which never re-run the baseline above.
	if err := migrateAdditiveColumns(db); err != nil {
		return fmt.Errorf("additive columns: %w", err)
	}
	return nil
}
