package main

import (
	"context"
	"database/sql"
	"strings"
)

// SQLite cannot alter a CHECK constraint, so rebuild the DDNS tables when
// upgrading an installation that predates CNAME support.
func migrateDDNSRecordType(db *sql.DB) error {
	var definition string
	if err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name='ddns_records'`).Scan(&definition); err != nil {
		return err
	}
	if strings.Contains(strings.ToUpper(definition), "'CNAME'") {
		return nil
	}
	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, `PRAGMA foreign_keys=OFF`); err != nil {
		return err
	}
	defer func() { _, _ = conn.ExecContext(ctx, `PRAGMA foreign_keys=ON`) }()
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	statements := []string{
		`DROP INDEX IF EXISTS idx_ddns_runs_record_id`,
		`ALTER TABLE ddns_runs RENAME TO ddns_runs_legacy`,
		`ALTER TABLE ddns_records RENAME TO ddns_records_legacy`,
		`CREATE TABLE ddns_records(id INTEGER PRIMARY KEY, name TEXT NOT NULL, dns_provider_id INTEGER NOT NULL REFERENCES dns_providers(id), zone TEXT NOT NULL, record_name TEXT NOT NULL, record_type TEXT NOT NULL CHECK(record_type IN ('A','AAAA','CNAME')), cname_target TEXT NOT NULL DEFAULT '', ttl_seconds INTEGER NOT NULL DEFAULT 300, interval_minutes INTEGER NOT NULL DEFAULT 5, enabled INTEGER NOT NULL DEFAULT 1, last_ip TEXT NOT NULL DEFAULT '', last_run_at INTEGER NOT NULL DEFAULT 0, next_run_at INTEGER NOT NULL DEFAULT 0, last_error TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP, updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP, ip_source TEXT NOT NULL DEFAULT 'http', interface_name TEXT NOT NULL DEFAULT '', interface_ip TEXT NOT NULL DEFAULT '')`,
		`INSERT INTO ddns_records(id,name,dns_provider_id,zone,record_name,record_type,cname_target,ttl_seconds,interval_minutes,enabled,last_ip,last_run_at,next_run_at,last_error,created_at,updated_at,ip_source,interface_name,interface_ip) SELECT id,name,dns_provider_id,zone,record_name,record_type,cname_target,ttl_seconds,interval_minutes,enabled,last_ip,last_run_at,next_run_at,last_error,created_at,updated_at,ip_source,interface_name,interface_ip FROM ddns_records_legacy`,
		`CREATE TABLE ddns_runs(id INTEGER PRIMARY KEY, ddns_record_id INTEGER NOT NULL REFERENCES ddns_records(id) ON DELETE CASCADE, observed_ip TEXT NOT NULL DEFAULT '', status TEXT NOT NULL, error_text TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP)`,
		`INSERT INTO ddns_runs(id,ddns_record_id,observed_ip,status,error_text,created_at) SELECT id,ddns_record_id,observed_ip,status,error_text,created_at FROM ddns_runs_legacy`,
		`DROP TABLE ddns_runs_legacy`,
		`DROP TABLE ddns_records_legacy`,
		`CREATE INDEX idx_ddns_runs_record_id ON ddns_runs(ddns_record_id,id DESC)`,
	}
	for _, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return tx.Commit()
}
