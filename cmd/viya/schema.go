package main

import (
	"database/sql"
	"log/slog"
)

func migrateSchema(db *sql.DB) error {
	var err error
	if _, err = db.Exec(`PRAGMA foreign_keys=ON; PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000;
	CREATE TABLE IF NOT EXISTS proxy_hosts(id INTEGER PRIMARY KEY, domain TEXT NOT NULL UNIQUE, upstream TEXT NOT NULL, enabled INTEGER NOT NULL DEFAULT 1, version INTEGER NOT NULL DEFAULT 1, created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP, updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP);
	CREATE TABLE IF NOT EXISTS config_revisions(id INTEGER PRIMARY KEY, status TEXT NOT NULL, config_json TEXT NOT NULL, error_text TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP);
	CREATE TABLE IF NOT EXISTS dns_providers(id INTEGER PRIMARY KEY, name TEXT NOT NULL, provider_type TEXT NOT NULL, credentials_ciphertext BLOB NOT NULL, credentials_nonce BLOB NOT NULL, key_version INTEGER NOT NULL DEFAULT 1, created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP, updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP);
	CREATE TABLE IF NOT EXISTS users(id INTEGER PRIMARY KEY, username TEXT NOT NULL UNIQUE, password_hash BLOB NOT NULL, created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP);
	CREATE TABLE IF NOT EXISTS sessions(token_hash BLOB PRIMARY KEY, user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE, csrf_token TEXT NOT NULL, expires_at INTEGER NOT NULL, created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP);`); err != nil {
		slog.Error("migrate", "error", err)
		return err
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS panel_settings(key TEXT PRIMARY KEY,value TEXT NOT NULL)`); err != nil {
		slog.Error("migrate panel settings", "error", err)
		return err
	}
	// Append-only authentication trail. created_at is Unix seconds rather than
	// CURRENT_TIMESTAMP so the UI never has to guess the timezone.
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS login_audit(id INTEGER PRIMARY KEY, username TEXT NOT NULL DEFAULT '', ip TEXT NOT NULL DEFAULT '', user_agent TEXT NOT NULL DEFAULT '', result TEXT NOT NULL, reason TEXT NOT NULL DEFAULT '', created_at INTEGER NOT NULL DEFAULT 0)`); err != nil {
		slog.Error("migrate login audit", "error", err)
		return err
	}
	// Login throttling state lives in SQLite so a service restart cannot be
	// used by an attacker to reset the window and resume guessing passwords.
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS login_attempts(ip TEXT PRIMARY KEY, window_start INTEGER NOT NULL DEFAULT 0, count INTEGER NOT NULL DEFAULT 0)`); err != nil {
		slog.Error("migrate login attempts", "error", err)
		return err
	}
	// The service-monitor feature is gone, but its tables are still part of the
	// configuration archive format. Creating them keeps archives exported before
	// the removal importable; nothing reads them any more.
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS monitors(id INTEGER PRIMARY KEY,name TEXT NOT NULL,kind TEXT NOT NULL,target TEXT NOT NULL,interval INTEGER NOT NULL DEFAULT 60,timeout INTEGER NOT NULL DEFAULT 5,enabled INTEGER NOT NULL DEFAULT 1,next_check INTEGER NOT NULL DEFAULT 0,last_check INTEGER NOT NULL DEFAULT 0,status TEXT NOT NULL DEFAULT 'pending',latency REAL NOT NULL DEFAULT 0,error TEXT NOT NULL DEFAULT '');
		CREATE TABLE IF NOT EXISTS monitor_samples(id INTEGER PRIMARY KEY,monitor_id INTEGER NOT NULL REFERENCES monitors(id) ON DELETE CASCADE,ok INTEGER NOT NULL,latency REAL NOT NULL,at INTEGER NOT NULL);
		CREATE INDEX IF NOT EXISTS idx_monitor_samples ON monitor_samples(monitor_id,id DESC)`); err != nil {
		slog.Error("migrate legacy monitor tables", "error", err)
		return err
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS layer4_rules(id INTEGER PRIMARY KEY, name TEXT NOT NULL, listen_address TEXT NOT NULL DEFAULT "0.0.0.0", listen_port INTEGER NOT NULL, upstream TEXT NOT NULL, protocol TEXT NOT NULL, enabled INTEGER NOT NULL DEFAULT 1, version INTEGER NOT NULL DEFAULT 1, created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP, updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP)`); err != nil {
		slog.Error("migrate layer4 rules", "error", err)
		return err
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS ddns_records(id INTEGER PRIMARY KEY, name TEXT NOT NULL, dns_provider_id INTEGER NOT NULL REFERENCES dns_providers(id), zone TEXT NOT NULL, record_name TEXT NOT NULL, record_type TEXT NOT NULL CHECK(record_type IN ('A','AAAA','CNAME')), cname_target TEXT NOT NULL DEFAULT '', ttl_seconds INTEGER NOT NULL DEFAULT 300, interval_minutes INTEGER NOT NULL DEFAULT 5, enabled INTEGER NOT NULL DEFAULT 1, last_ip TEXT NOT NULL DEFAULT '', last_run_at INTEGER NOT NULL DEFAULT 0, next_run_at INTEGER NOT NULL DEFAULT 0, last_error TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP, updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP); CREATE TABLE IF NOT EXISTS ddns_runs(id INTEGER PRIMARY KEY, ddns_record_id INTEGER NOT NULL REFERENCES ddns_records(id) ON DELETE CASCADE, observed_ip TEXT NOT NULL DEFAULT '', status TEXT NOT NULL, error_text TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP); CREATE INDEX IF NOT EXISTS idx_ddns_runs_record_id ON ddns_runs(ddns_record_id,id DESC);`); err != nil {
		slog.Error("migrate DDNS records", "error", err)
		return err
	}
	for _, column := range []struct{ name, definition string }{{"ip_source", "TEXT NOT NULL DEFAULT 'http'"}, {"interface_name", "TEXT NOT NULL DEFAULT ''"}, {"interface_ip", "TEXT NOT NULL DEFAULT ''"}, {"cname_target", "TEXT NOT NULL DEFAULT ''"}} {
		if err := ensureColumn(db, "ddns_records", column.name, column.definition); err != nil {
			slog.Error("database schema update", "table", "ddns_records", "column", column.name, "error", err)
			return err
		}
	}
	if err := migrateDDNSRecordType(db); err != nil {
		slog.Error("database schema update", "table", "ddns_records", "error", err)
		return err
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS custom_certificates(id INTEGER PRIMARY KEY, name TEXT NOT NULL, cert_path TEXT NOT NULL, key_path TEXT NOT NULL, dns_names_json TEXT NOT NULL, not_after TEXT NOT NULL, created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP)`); err != nil {
		slog.Error("migrate custom certificates", "error", err)
		return err
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS ip_bans(id INTEGER PRIMARY KEY, domain TEXT NOT NULL, ip TEXT NOT NULL, reason TEXT NOT NULL, expires_at INTEGER NOT NULL, created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP, UNIQUE(domain,ip))`); err != nil {
		slog.Error("migrate IP bans", "error", err)
		return err
	}
	if err := ensureColumn(db, "proxy_hosts", "custom_certificate_id", "INTEGER REFERENCES custom_certificates(id)"); err != nil {
		slog.Error("database schema update", "column", "custom_certificate_id", "error", err)
		return err
	}
	for _, column := range []struct{ name, definition string }{
		{"name", "TEXT NOT NULL DEFAULT ''"},
		{"tls_mode", "TEXT NOT NULL DEFAULT 'auto'"},
		{"dns_provider_id", "INTEGER REFERENCES dns_providers(id)"},
		{"custom_headers", "TEXT NOT NULL DEFAULT '{}'"},
		{"basic_auth_user", "TEXT NOT NULL DEFAULT ''"},
		{"basic_auth_hash", "TEXT NOT NULL DEFAULT ''"},
		{"ip_allowlist", "TEXT NOT NULL DEFAULT '[]'"},
		{"ip_denylist", "TEXT NOT NULL DEFAULT '[]'"},
		{"rate_limit_requests", "INTEGER NOT NULL DEFAULT 0"},
		{"rate_limit_window", "TEXT NOT NULL DEFAULT ''"},
		{"https_port", "INTEGER NOT NULL DEFAULT 443"}, // retained for upgrades from older Viya versions
		{"path", "TEXT NOT NULL DEFAULT ''"},
		{"icon_color", "TEXT NOT NULL DEFAULT 'blue'"},
		// Site kinds share one handler chain; the type only selects the final
		// handler (reverse_proxy / file_server / static_response redirect).
		{"type", "TEXT NOT NULL DEFAULT 'proxy'"},
		{"root_path", "TEXT NOT NULL DEFAULT ''"},
		{"spa_fallback", "INTEGER NOT NULL DEFAULT 0"},
		{"browse", "INTEGER NOT NULL DEFAULT 0"},
		{"redirect_target", "TEXT NOT NULL DEFAULT ''"},
		{"redirect_status", "INTEGER NOT NULL DEFAULT 301"},
		{"redirect_keep_path", "INTEGER NOT NULL DEFAULT 1"},
		{"security_headers", "TEXT NOT NULL DEFAULT '{}'"},
		{"max_body_bytes", "INTEGER NOT NULL DEFAULT 0"},
		{"allowed_methods", "TEXT NOT NULL DEFAULT '[]'"},
		{"hide_server", "INTEGER NOT NULL DEFAULT 0"},
	} {
		if err := ensureColumn(db, "proxy_hosts", column.name, column.definition); err != nil {
			slog.Error("database schema update", "column", column.name, "error", err)
			return err
		}
	}
	if err := ensureColumn(db, "ddns_records", "comment", "TEXT NOT NULL DEFAULT 'viya'"); err != nil {
		slog.Error("migrate DDNS comment", "error", err)
		return err
	}
	// Two-factor authentication was removed; drop the column it used to live in
	// so no dormant secret lingers in the database. Installs created without the
	// column simply report an error here, which is safe to ignore.
	if _, err := db.Exec(`ALTER TABLE users DROP COLUMN totp_secret`); err != nil {
		slog.Debug("drop legacy two-factor column", "error", err)
	}
	if err := ensureColumn(db, "users", "role", "TEXT NOT NULL DEFAULT 'admin'"); err != nil {
		slog.Error("migrate user role", "error", err)
		return err
	}

	return nil
}
