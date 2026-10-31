package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"modernc.org/sqlite"
	"viya/internal/secretstore"
	"viya/internal/web"
)

type Host struct {
	ID                  int64              `json:"id"`
	Name                string             `json:"name"`
	Domain              string             `json:"domain"`
	ListenerID          string             `json:"listener_id"`
	CertificateSource   *CertificateSource `json:"certificate_source,omitempty"`
	Upstream            string             `json:"upstream"`
	Enabled             bool               `json:"enabled"`
	TLSMode             string             `json:"tls_mode"`
	Path                string             `json:"path"`
	IconColor           string             `json:"icon_color"`
	DNSProviderID       *int64             `json:"dns_provider_id,omitempty"`
	CustomCertificateID *int64             `json:"custom_certificate_id,omitempty"`
	CustomHeaders       map[string]string  `json:"custom_headers"`
	BasicAuthEnabled    bool               `json:"basic_auth_enabled"`
	BasicAuthUser       string             `json:"basic_auth_user,omitempty"`
	BasicAuthPassword   string             `json:"basic_auth_password,omitempty"`
	PasswordHash        string             `json:"-"`
	IPAllowlist         []string           `json:"ip_allowlist"`
	IPDenylist          []string           `json:"ip_denylist"`
	RateLimitRequests   int                `json:"rate_limit_requests"`
	RateLimitWindow     string             `json:"rate_limit_window,omitempty"`
	Type                string             `json:"type"`
	RootPath            string             `json:"root_path,omitempty"`
	SpaFallback         bool               `json:"spa_fallback,omitempty"`
	Browse              bool               `json:"browse,omitempty"`
	RedirectTarget      string             `json:"redirect_target,omitempty"`
	RedirectStatus      int                `json:"redirect_status,omitempty"`
	RedirectKeepPath    bool               `json:"redirect_keep_path"`
	Version             int64              `json:"version"`
}

type Layer4Rule struct {
	ID            int64  `json:"id"`
	Name          string `json:"name"`
	ListenAddress string `json:"listen_address"`
	ListenPort    int    `json:"listen_port"`
	Upstream      string `json:"upstream_address"`
	Protocol      string `json:"protocol"`
	Enabled       bool   `json:"enabled"`
	Version       int64  `json:"version"`
}

type App struct {
	configMu       sync.RWMutex
	listenerMu     sync.Mutex
	db             *sql.DB
	adminURL       string
	adminListen    string
	httpListen     string
	caddyBin       string
	mu             sync.Mutex
	secureCookies  bool
	trustedProxies []*net.IPNet
	ctx            context.Context
	caddyReady     atomic.Bool
	masterKey      []byte
	ddnsMu         sync.Mutex
	certProgressMu sync.RWMutex
	certProgress   map[string]certificateProgress
	dataDir        string
	failCounts     map[string][]time.Time
	applyKick      chan struct{}
	ddnsRunning    map[int64]bool
	ddnsFailures   map[int64]int
	alertMu        sync.Mutex
	alertLast      map[string]time.Time
	healthMu       sync.RWMutex
	health         map[int64]upstreamHealth
}

var domainRE = regexp.MustCompile(`(?i)^(?:\*\.)?(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)*[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
var headerNameRE = regexp.MustCompile(`^[A-Za-z0-9!#$%&'*+.^_` + "`" + `|~-]+$`)

func main() {
	runCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	dataDir := env("VIYA_DATA_DIR", "./data")
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		slog.Error("create data dir", "error", err)
		os.Exit(1)
	}
	if err := migrateLegacyDatabaseFilename(dataDir); err != nil {
		slog.Error("migrate legacy database filename", "error", err)
		os.Exit(1)
	}
	db, err := sql.Open("sqlite", filepath.Join(dataDir, "viya.db"))
	if err != nil {
		slog.Error("open database", "error", err)
		os.Exit(1)
	}
	db.SetMaxOpenConns(1)
	// SQLite is single-writer; one connection keeps transactions serialized but
	// means a long write (configuration import, traffic ingestion) briefly
	// queues every other request. Keep writes short and batch them where
	// possible instead of raising this limit.
	if _, err = db.Exec(`PRAGMA foreign_keys=ON; PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000;
	CREATE TABLE IF NOT EXISTS proxy_hosts(id INTEGER PRIMARY KEY, domain TEXT NOT NULL UNIQUE, upstream TEXT NOT NULL, enabled INTEGER NOT NULL DEFAULT 1, version INTEGER NOT NULL DEFAULT 1, created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP, updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP);
	CREATE TABLE IF NOT EXISTS config_revisions(id INTEGER PRIMARY KEY, status TEXT NOT NULL, config_json TEXT NOT NULL, error_text TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP);
	CREATE TABLE IF NOT EXISTS dns_providers(id INTEGER PRIMARY KEY, name TEXT NOT NULL, provider_type TEXT NOT NULL, credentials_ciphertext BLOB NOT NULL, credentials_nonce BLOB NOT NULL, key_version INTEGER NOT NULL DEFAULT 1, created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP, updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP);
	CREATE TABLE IF NOT EXISTS users(id INTEGER PRIMARY KEY, username TEXT NOT NULL UNIQUE, password_hash BLOB NOT NULL, created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP);
	CREATE TABLE IF NOT EXISTS sessions(token_hash BLOB PRIMARY KEY, user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE, csrf_token TEXT NOT NULL, expires_at INTEGER NOT NULL, created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP);`); err != nil {
		slog.Error("migrate", "error", err)
		os.Exit(1)
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS panel_settings(key TEXT PRIMARY KEY,value TEXT NOT NULL)`); err != nil {
		slog.Error("migrate panel settings", "error", err)
		os.Exit(1)
	}
	// Append-only authentication trail. created_at is Unix seconds rather than
	// CURRENT_TIMESTAMP so the UI never has to guess the timezone.
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS login_audit(id INTEGER PRIMARY KEY, username TEXT NOT NULL DEFAULT '', ip TEXT NOT NULL DEFAULT '', user_agent TEXT NOT NULL DEFAULT '', result TEXT NOT NULL, reason TEXT NOT NULL DEFAULT '', created_at INTEGER NOT NULL DEFAULT 0)`); err != nil {
		slog.Error("migrate login audit", "error", err)
		os.Exit(1)
	}
	// Login throttling state lives in SQLite so a service restart cannot be
	// used by an attacker to reset the window and resume guessing passwords.
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS login_attempts(ip TEXT PRIMARY KEY, window_start INTEGER NOT NULL DEFAULT 0, count INTEGER NOT NULL DEFAULT 0)`); err != nil {
		slog.Error("migrate login attempts", "error", err)
		os.Exit(1)
	}
	// The service-monitor feature is gone, but its tables are still part of the
	// configuration archive format. Creating them keeps archives exported before
	// the removal importable; nothing reads them any more.
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS monitors(id INTEGER PRIMARY KEY,name TEXT NOT NULL,kind TEXT NOT NULL,target TEXT NOT NULL,interval INTEGER NOT NULL DEFAULT 60,timeout INTEGER NOT NULL DEFAULT 5,enabled INTEGER NOT NULL DEFAULT 1,next_check INTEGER NOT NULL DEFAULT 0,last_check INTEGER NOT NULL DEFAULT 0,status TEXT NOT NULL DEFAULT 'pending',latency REAL NOT NULL DEFAULT 0,error TEXT NOT NULL DEFAULT '');
		CREATE TABLE IF NOT EXISTS monitor_samples(id INTEGER PRIMARY KEY,monitor_id INTEGER NOT NULL REFERENCES monitors(id) ON DELETE CASCADE,ok INTEGER NOT NULL,latency REAL NOT NULL,at INTEGER NOT NULL);
		CREATE INDEX IF NOT EXISTS idx_monitor_samples ON monitor_samples(monitor_id,id DESC)`); err != nil {
		slog.Error("migrate legacy monitor tables", "error", err)
		os.Exit(1)
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS layer4_rules(id INTEGER PRIMARY KEY, name TEXT NOT NULL, listen_address TEXT NOT NULL DEFAULT "0.0.0.0", listen_port INTEGER NOT NULL, upstream TEXT NOT NULL, protocol TEXT NOT NULL, enabled INTEGER NOT NULL DEFAULT 1, version INTEGER NOT NULL DEFAULT 1, created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP, updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP)`); err != nil {
		slog.Error("migrate layer4 rules", "error", err)
		os.Exit(1)
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS ddns_records(id INTEGER PRIMARY KEY, name TEXT NOT NULL, dns_provider_id INTEGER NOT NULL REFERENCES dns_providers(id), zone TEXT NOT NULL, record_name TEXT NOT NULL, record_type TEXT NOT NULL CHECK(record_type IN ('A','AAAA','CNAME')), cname_target TEXT NOT NULL DEFAULT '', ttl_seconds INTEGER NOT NULL DEFAULT 300, interval_minutes INTEGER NOT NULL DEFAULT 5, enabled INTEGER NOT NULL DEFAULT 1, last_ip TEXT NOT NULL DEFAULT '', last_run_at INTEGER NOT NULL DEFAULT 0, next_run_at INTEGER NOT NULL DEFAULT 0, last_error TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP, updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP); CREATE TABLE IF NOT EXISTS ddns_runs(id INTEGER PRIMARY KEY, ddns_record_id INTEGER NOT NULL REFERENCES ddns_records(id) ON DELETE CASCADE, observed_ip TEXT NOT NULL DEFAULT '', status TEXT NOT NULL, error_text TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP); CREATE INDEX IF NOT EXISTS idx_ddns_runs_record_id ON ddns_runs(ddns_record_id,id DESC);`); err != nil {
		slog.Error("migrate DDNS records", "error", err)
		os.Exit(1)
	}
	for _, column := range []struct{ name, definition string }{{"ip_source", "TEXT NOT NULL DEFAULT 'http'"}, {"interface_name", "TEXT NOT NULL DEFAULT ''"}, {"interface_ip", "TEXT NOT NULL DEFAULT ''"}, {"cname_target", "TEXT NOT NULL DEFAULT ''"}} {
		if err := ensureColumn(db, "ddns_records", column.name, column.definition); err != nil {
			slog.Error("database schema update", "table", "ddns_records", "column", column.name, "error", err)
			os.Exit(1)
		}
	}
	if err := migrateDDNSRecordType(db); err != nil {
		slog.Error("database schema update", "table", "ddns_records", "error", err)
		os.Exit(1)
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS custom_certificates(id INTEGER PRIMARY KEY, name TEXT NOT NULL, cert_path TEXT NOT NULL, key_path TEXT NOT NULL, dns_names_json TEXT NOT NULL, not_after TEXT NOT NULL, created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP)`); err != nil {
		slog.Error("migrate custom certificates", "error", err)
		os.Exit(1)
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS ip_bans(id INTEGER PRIMARY KEY, domain TEXT NOT NULL, ip TEXT NOT NULL, reason TEXT NOT NULL, expires_at INTEGER NOT NULL, created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP, UNIQUE(domain,ip))`); err != nil {
		slog.Error("migrate IP bans", "error", err)
		os.Exit(1)
	}
	if err := ensureColumn(db, "proxy_hosts", "custom_certificate_id", "INTEGER REFERENCES custom_certificates(id)"); err != nil {
		slog.Error("database schema update", "column", "custom_certificate_id", "error", err)
		os.Exit(1)
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
	} {
		if err := ensureColumn(db, "proxy_hosts", column.name, column.definition); err != nil {
			slog.Error("database schema update", "column", column.name, "error", err)
			os.Exit(1)
		}
	}
	if err := ensureColumn(db, "ddns_records", "comment", "TEXT NOT NULL DEFAULT 'viya'"); err != nil {
		slog.Error("migrate DDNS comment", "error", err)
		os.Exit(1)
	}
	// Two-factor authentication was removed; drop the column it used to live in
	// so no dormant secret lingers in the database. Installs created without the
	// column simply report an error here, which is safe to ignore.
	if _, err := db.Exec(`ALTER TABLE users DROP COLUMN totp_secret`); err != nil {
		slog.Debug("drop legacy two-factor column", "error", err)
	}
	if err := ensureColumn(db, "users", "role", "TEXT NOT NULL DEFAULT 'admin'"); err != nil {
		slog.Error("migrate user role", "error", err)
		os.Exit(1)
	}
	masterKeyPath := env("VIYA_MASTER_KEY_FILE", filepath.Join(dataDir, "master.key"))
	if err := os.MkdirAll(filepath.Dir(masterKeyPath), 0700); err != nil {
		slog.Error("create master key directory", "error", err)
		os.Exit(1)
	}
	masterKey, err := secretstore.LoadOrCreateKey(masterKeyPath)
	if err != nil {
		slog.Error("load master key", "error", err)
		os.Exit(1)
	}
	if err := migrateDNSProviderAAD(db, masterKey); err != nil {
		slog.Error("migrate DNS provider encryption namespace", "error", err)
		os.Exit(1)
	}
	secureCookies := env("VIYA_COOKIE_SECURE", "false") == "true"
	trustedProxies, err := parseTrustedProxies(env("VIYA_TRUSTED_PROXIES", "127.0.0.1,::1"))
	if err != nil {
		slog.Error("invalid trusted proxy list", "error", err)
		os.Exit(1)
	}
	adminURL := env("VIYA_CADDY_ADMIN", "http://127.0.0.1:2019")
	adminListen, err := caddyAdminListen(adminURL)
	if err != nil {
		slog.Error("invalid Caddy Admin API address", "error", err)
		os.Exit(1)
	}
	httpListen := env("VIYA_HTTP_LISTEN", ":8080")
	if err := validateListenAddress(httpListen); err != nil {
		slog.Error("invalid Caddy HTTP listener", "error", err)
		os.Exit(1)
	}
	app := &App{db: db, adminURL: adminURL, adminListen: adminListen, httpListen: httpListen, caddyBin: env("VIYA_CADDY_BIN", filepath.Join("bin", "caddy")), secureCookies: secureCookies, trustedProxies: trustedProxies, ctx: runCtx, masterKey: masterKey, dataDir: dataDir, failCounts: make(map[string][]time.Time), certProgress: make(map[string]certificateProgress), applyKick: make(chan struct{}, 1), ddnsRunning: make(map[int64]bool)}
	app.configureProgramLogging()
	if err := app.initListenerSites(); err != nil {
		slog.Error("migrate listener sites", "error", err)
		os.Exit(1)
	}
	if err := app.initTraffic(); err != nil {
		slog.Error("initialize traffic statistics", "error", err)
		os.Exit(1)
	}
	if err := app.initCertificateRequests(); err != nil {
		slog.Error("initialize certificate requests", "error", err)
		os.Exit(1)
	}
	if err := ensureColumn(db, "ip_bans", "source", "TEXT NOT NULL DEFAULT 'auto'"); err != nil {
		slog.Error("initialize editable bans", "error", err)
		os.Exit(1)
	}
	if err := app.startCaddy(dataDir); err != nil {
		slog.Warn("Caddy is not ready; API will start in degraded mode", "error", err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"ok": true, "caddy_ready": app.isCaddyReady(), "time": time.Now().UTC()})
	})
	mux.Handle("GET /api/v1/layer4-rules", app.requireAuth(http.HandlerFunc(app.listLayer4Rules)))
	mux.Handle("POST /api/v1/layer4-rules", app.requireAuth(http.HandlerFunc(app.createLayer4Rule)))
	mux.Handle("PUT /api/v1/layer4-rules/{id}", app.requireAuth(http.HandlerFunc(app.updateLayer4Rule)))
	mux.Handle("DELETE /api/v1/layer4-rules/{id}", app.requireAuth(http.HandlerFunc(app.deleteLayer4Rule)))
	mux.Handle("GET /api/v1/layer4-rules/{id}/logs", app.requireAuth(http.HandlerFunc(app.layer4RuleLogs)))
	mux.Handle("GET /api/v1/ddns-records", app.requireAuth(http.HandlerFunc(app.listDDNSRecords)))
	mux.Handle("GET /api/v1/network-interfaces", app.requireAuth(http.HandlerFunc(app.listNetworkInterfaces)))
	mux.Handle("POST /api/v1/ddns-records", app.requireAuth(http.HandlerFunc(app.createDDNSRecord)))
	mux.Handle("PUT /api/v1/ddns-records/{id}", app.requireAuth(http.HandlerFunc(app.updateDDNSRecord)))
	mux.Handle("DELETE /api/v1/ddns-records/{id}", app.requireAuth(http.HandlerFunc(app.deleteDDNSRecord)))
	mux.Handle("POST /api/v1/ddns-records/{id}/run", app.requireAuth(http.HandlerFunc(app.runDDNSRecord)))
	mux.Handle("GET /api/v1/ddns-records/{id}/runs", app.requireAuth(http.HandlerFunc(app.listDDNSRuns)))
	mux.Handle("GET /api/v1/custom-certificates", app.requireAuth(http.HandlerFunc(app.listCustomCertificates)))
	mux.Handle("POST /api/v1/custom-certificates", app.requireAuth(http.HandlerFunc(app.createCustomCertificate)))
	mux.Handle("DELETE /api/v1/custom-certificates/{id}", app.requireAuth(http.HandlerFunc(app.deleteCustomCertificate)))
	mux.Handle("GET /api/v1/logs", app.requireAuth(http.HandlerFunc(app.recentLogs)))
	mux.Handle("GET /api/v1/logs/program", app.requireAuth(http.HandlerFunc(app.recentProgramLogs)))
	mux.Handle("GET /api/v1/logs/settings", app.requireAuth(http.HandlerFunc(app.getLogSettings)))
	mux.Handle("PUT /api/v1/logs/settings", app.requireAuth(http.HandlerFunc(app.updateLogSettings)))
	mux.Handle("PATCH /api/v1/layer4-rules/{id}/enabled", app.requireAuth(http.HandlerFunc(app.toggleLayer4Enabled)))
	mux.Handle("GET /api/v1/proxy-hosts/traffic", app.requireAuth(http.HandlerFunc(app.siteTraffic)))
	mux.Handle("GET /api/v1/layer4-rules/traffic", app.requireAuth(http.HandlerFunc(app.l4Traffic)))
	mux.Handle("GET /api/v1/proxy-hosts/health", app.requireAuth(http.HandlerFunc(app.upstreamHealthReport)))
	mux.Handle("POST /api/v1/proxy-hosts/health", app.requireAuth(http.HandlerFunc(app.forceProbeUpstreams)))
	mux.Handle("GET /api/v1/alert-settings", app.requireAuth(http.HandlerFunc(app.getAlertSettings)))
	mux.Handle("PUT /api/v1/alert-settings", app.requireAuth(http.HandlerFunc(app.updateAlertSettings)))
	mux.Handle("POST /api/v1/alert-settings/test", app.requireAuth(http.HandlerFunc(app.testAlertSettings)))
	mux.Handle("GET /api/v1/certificates/expiring", app.requireAuth(http.HandlerFunc(app.expiringCertificates)))
	mux.Handle("GET /api/v1/proxy-hosts/{id}/logs", app.requireAuth(http.HandlerFunc(app.siteAccessLogs)))
	// Static-site file management (list/upload/download/delete/mkdir). Writes
	// are limited to the managed sites tree and stay admin-only via requireAuth.
	mux.Handle("GET /api/v1/proxy-hosts/{id}/files", app.requireAuth(http.HandlerFunc(app.listSiteFiles)))
	mux.Handle("GET /api/v1/proxy-hosts/{id}/files/download", app.requireAuth(http.HandlerFunc(app.downloadSiteFile)))
	mux.Handle("POST /api/v1/proxy-hosts/{id}/files/upload", app.requireAuth(http.HandlerFunc(app.uploadSiteFiles)))
	mux.Handle("POST /api/v1/proxy-hosts/{id}/files/mkdir", app.requireAuth(http.HandlerFunc(app.mkdirSiteFile)))
	mux.Handle("DELETE /api/v1/proxy-hosts/{id}/files", app.requireAuth(http.HandlerFunc(app.deleteSiteFile)))
	mux.Handle("GET /api/v1/logs/stream", app.requireAuth(http.HandlerFunc(app.streamLogs)))
	mux.Handle("GET /api/v1/ip-bans", app.requireAuth(http.HandlerFunc(app.listIPBans)))
	mux.Handle("POST /api/v1/ip-bans", app.requireAuth(http.HandlerFunc(app.saveIPBan)))
	mux.Handle("PUT /api/v1/ip-bans/{id}", app.requireAuth(http.HandlerFunc(app.saveIPBan)))
	mux.Handle("GET /api/v1/managed-certificates", app.requireAuth(http.HandlerFunc(app.listManagedCertificates)))
	mux.Handle("POST /api/v1/managed-certificates", app.requireAuth(http.HandlerFunc(app.requestCertificate)))
	mux.Handle("PUT /api/v1/managed-certificates/{id}", app.requireAuth(http.HandlerFunc(app.requestCertificate)))
	mux.Handle("GET /api/v1/system-metrics", app.requireAuth(http.HandlerFunc(app.systemMetrics)))
	mux.Handle("PATCH /api/v1/proxy-hosts/{id}/enabled", app.requireAuth(http.HandlerFunc(app.toggleHostEnabled)))
	mux.Handle("DELETE /api/v1/managed-certificates/{id}", app.requireAuth(http.HandlerFunc(app.deleteManagedCertificate)))
	mux.Handle("DELETE /api/v1/ip-bans/{id}", app.requireAuth(http.HandlerFunc(app.removeIPBan)))
	mux.Handle("GET /api/v1/version", app.requireAuth(http.HandlerFunc(app.versionInfoHandler)))
	mux.Handle("POST /api/v1/version/update", app.requireAuth(http.HandlerFunc(app.versionUpdateHandler)))
	go app.ddnsWorker()
	go app.healthWorker()
	go app.watchAccessLogs()
	go app.watchBanExpiry()
	go app.watchTraffic()
	go app.applyWorker()
	mux.HandleFunc("GET /api/v1/auth/status", app.authStatus)
	mux.HandleFunc("POST /api/v1/auth/setup", app.setupAdmin)
	mux.HandleFunc("POST /api/v1/auth/login", app.login)
	mux.Handle("POST /api/v1/auth/logout", app.requireAuth(http.HandlerFunc(app.logout)))
	mux.Handle("GET /api/v1/auth/me", app.requireAuth(http.HandlerFunc(app.me)))
	mux.Handle("GET /api/v1/auth/audit", app.requireAuth(http.HandlerFunc(app.listLoginAudit)))
	mux.Handle("GET /api/v1/panel/settings", app.requireAuth(http.HandlerFunc(app.getPanelSettings)))
	mux.Handle("PUT /api/v1/panel/settings/port", app.requireAuth(http.HandlerFunc(app.updatePanelPort)))
	mux.Handle("POST /api/v1/auth/sessions/revoke-others", app.requireAuth(http.HandlerFunc(app.revokeOtherSessions)))
	mux.Handle("POST /api/v1/auth/password", app.requireAuth(http.HandlerFunc(app.changeAdminPassword)))
	mux.Handle("GET /api/v1/proxy-hosts", app.requireAuth(http.HandlerFunc(app.listHosts)))
	mux.Handle("GET /api/v1/config/revisions", app.requireAuth(http.HandlerFunc(app.listRevisions)))
	mux.Handle("GET /api/v1/config/revisions/{id}", app.requireAuth(http.HandlerFunc(app.configRevision)))
	mux.Handle("GET /api/v1/config/revisions/{id}/diff", app.requireAuth(http.HandlerFunc(app.configRevisionDiff)))
	mux.Handle("POST /api/v1/config/apply", app.requireAuth(http.HandlerFunc(app.reapplyConfig)))
	mux.Handle("GET /api/v1/dns-providers", app.requireAuth(http.HandlerFunc(app.listDNSProviders)))
	mux.Handle("GET /api/v1/dns-providers/{id}/zones", app.requireAuth(http.HandlerFunc(app.listDNSZones)))
	mux.Handle("POST /api/v1/dns-providers", app.requireAuth(http.HandlerFunc(app.createDNSProvider)))
	mux.Handle("PUT /api/v1/dns-providers/{id}", app.requireAuth(http.HandlerFunc(app.updateDNSProvider)))
	mux.Handle("DELETE /api/v1/dns-providers/{id}", app.requireAuth(http.HandlerFunc(app.deleteDNSProvider)))
	mux.Handle("POST /api/v1/proxy-hosts", app.requireAuth(http.HandlerFunc(app.createHost)))
	mux.Handle("POST /api/v1/proxy-hosts/test-upstream", app.requireAuth(http.HandlerFunc(app.testUpstream)))
	mux.Handle("PUT /api/v1/proxy-hosts/{id}", app.requireAuth(http.HandlerFunc(app.updateHost)))
	mux.Handle("GET /api/v1/certificate-inventory", app.requireAuth(http.HandlerFunc(app.certificateInventory)))
	mux.Handle("DELETE /api/v1/certificate-inventory/{domain}", app.requireAuth(http.HandlerFunc(app.deleteAutomaticCertificate)))
	mux.Handle("POST /api/v1/config/export", app.requireAuth(http.HandlerFunc(app.exportConfiguration)))
	mux.Handle("POST /api/v1/config/import/inspect", app.requireAuth(http.HandlerFunc(app.inspectConfiguration)))
	mux.Handle("POST /api/v1/config/import", app.requireAuthUnlocked(http.HandlerFunc(app.importConfiguration)))
	mux.Handle("POST /api/v1/auth/username", app.requireAuth(http.HandlerFunc(app.changeUsername)))
	mux.Handle("GET /api/v1/users", app.requireAuth(http.HandlerFunc(app.listUsers)))
	mux.Handle("POST /api/v1/users", app.requireAuth(http.HandlerFunc(app.createUser)))
	mux.Handle("PUT /api/v1/users/{id}", app.requireAuth(http.HandlerFunc(app.updateUser)))
	mux.Handle("DELETE /api/v1/users/{id}", app.requireAuth(http.HandlerFunc(app.deleteUser)))
	mux.Handle("GET /api/v1/web-listeners", app.requireAuth(http.HandlerFunc(app.getWebListeners)))
	mux.Handle("PUT /api/v1/web-listeners", app.requireAuth(http.HandlerFunc(app.saveWebListeners)))
	mux.Handle("GET /api/v1/web-listeners/check", app.requireAuth(http.HandlerFunc(app.checkWebListener)))
	mux.Handle("PATCH /api/v1/ddns-records/{id}/enabled", app.requireAuth(http.HandlerFunc(app.toggleDDNSEnabled)))
	mux.Handle("DELETE /api/v1/proxy-hosts/{id}", app.requireAuth(http.HandlerFunc(app.deleteHost)))
	static := web.Handler()
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		static.ServeHTTP(w, r)
	})
	addr := env("VIYA_LISTEN", "127.0.0.1:8181")
	var configuredPort string
	if err := db.QueryRow(`SELECT value FROM panel_settings WHERE key='listen_port'`).Scan(&configuredPort); err == nil {
		host, _, splitErr := net.SplitHostPort(addr)
		if splitErr == nil {
			addr = net.JoinHostPort(host, configuredPort)
		}
	}
	// Bind before announcing readiness: a port clash must fail loudly with an
	// actionable message instead of logging "listening" and then dying.
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		slog.Error("panel port unavailable",
			"address", addr,
			"error", err,
			"hint", "another process is already bound to this address; stop it or change VIYA_LISTEN / the panel port setting")
		os.Exit(1)
	}
	slog.Info("Viya API listening", "address", addr)
	// WriteTimeout must stay above the slowest business operation (DDNS run 35s,
	// configuration import 90s); otherwise Go truncates a response after the
	// work already succeeded and operators retry the same import twice.
	server := &http.Server{Handler: securityHeaders(logRequests(mux)), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 120 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 1 << 20}
	serveErr := make(chan error, 1)
	go func() { serveErr <- server.Serve(listener) }()
	select {
	case err := <-serveErr:
		if !errors.Is(err, http.ErrServerClosed) {
			slog.Error("http server", "error", err)
			os.Exit(1)
		}
	case <-runCtx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			slog.Warn("HTTP server shutdown", "error", err)
		}
		<-serveErr
	}
}

func (a *App) startCaddy(dataDir string) error {
	bin := a.caddyBin
	if _, err := exec.LookPath(bin); err != nil {
		return fmt.Errorf("Caddy binary %q is unavailable", bin)
	}
	initialConfig, _ := json.Marshal(map[string]any{"admin": map[string]any{"listen": a.adminListen, "config": map[string]any{"persist": false}}, "apps": map[string]any{}})
	initial := initialConfig
	configPath := filepath.Join(dataDir, "bootstrap-caddy.json")
	if err := os.WriteFile(configPath, initial, 0600); err != nil {
		return err
	}
	ready := make(chan error, 1)
	go a.superviseCaddy(configPath, ready)
	select {
	case err := <-ready:
		return err
	case <-time.After(12 * time.Second):
		return errors.New("timed out waiting for Caddy Admin API")
	case <-a.ctx.Done():
		return errors.New("startup interrupted")
	}
}

func (a *App) superviseCaddy(configPath string, firstReady chan<- error) {
	first := true
	backoff := time.Second
	for a.ctx.Err() == nil {
		cmd := exec.Command(a.caddyBin, "run", "--config", configPath)
		stdoutLog := &caddyLogWriter{app: a, stream: "stdout"}
		stderrLog := &caddyLogWriter{app: a, stream: "stderr"}
		cmd.Stdout, cmd.Stderr = stdoutLog, stderrLog
		if err := cmd.Start(); err != nil {
			if first {
				firstReady <- err
				first = false
			}
			slog.Error("start Caddy", "error", err, "retry_in", backoff)
			if !waitContext(a.ctx, backoff) {
				return
			}
			backoff = min(backoff*2, 30*time.Second)
			continue
		}
		slog.Info("Caddy process started", "pid", cmd.Process.Pid)
		done := make(chan struct{})
		var processErr error
		go func() {
			processErr = cmd.Wait()
			stdoutLog.Flush()
			stderrLog.Flush()
			close(done)
		}()
		readyErr := waitAdmin(a.ctx, done, &processErr, a.adminURL, 10*time.Second)
		if readyErr == nil {
			a.setCaddyReady(true)
			if err := a.apply(a.ctx); err != nil {
				slog.Error("saved proxy rules could not be applied; inspect configuration history")
			} else {
				slog.Info("saved proxy rules reconciled")
			}
			if first {
				firstReady <- nil
				first = false
			}
			backoff = time.Second
		} else {
			a.setCaddyReady(false)
			if first {
				firstReady <- readyErr
				first = false
			}
			_ = cmd.Process.Kill()
		}
		select {
		case <-done:
			if a.ctx.Err() != nil {
				return
			}
			a.setCaddyReady(false)
			slog.Error("Caddy exited; restarting", "error", processErr, "retry_in", backoff)
			a.notify("caddy_exited", "Caddy 进程退出",
				fmt.Sprintf("代理引擎已退出，将在 %.0f 秒后重启。错误：%v", backoff.Seconds(), processErr))
		case <-a.ctx.Done():
			_ = cmd.Process.Signal(os.Interrupt)
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				_ = cmd.Process.Kill()
				<-done
			}
			return
		}
		if !waitContext(a.ctx, backoff) {
			return
		}
		backoff = min(backoff*2, 30*time.Second)
	}
}

func waitAdmin(ctx context.Context, done <-chan struct{}, processErr *error, adminURL string, timeout time.Duration) error {
	client := http.Client{Timeout: time.Second}
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		resp, err := client.Get(strings.TrimRight(adminURL, "/") + "/config/")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode < 500 {
				return nil
			}
		}
		select {
		case <-done:
			return fmt.Errorf("Caddy exited before Admin API became ready: %w", *processErr)
		case <-ticker.C:
		case <-deadline.C:
			return errors.New("timed out waiting for Caddy Admin API")
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func waitContext(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}

func (a *App) setCaddyReady(ready bool) {
	a.caddyReady.Store(ready)
}

func (a *App) isCaddyReady() bool {
	return a.caddyReady.Load()
}

func (a *App) listHosts(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.QueryContext(r.Context(), `SELECT id,name,domain,listener_id,upstream,enabled,version,tls_mode,path,icon_color,dns_provider_id,custom_certificate_id,custom_headers,basic_auth_user,basic_auth_hash,ip_allowlist,ip_denylist,rate_limit_requests,rate_limit_window,type,root_path,spa_fallback,browse,redirect_target,redirect_status,redirect_keep_path FROM proxy_hosts ORDER BY domain`)
	if err != nil {
		fail(w, 500, err)
		return
	}
	defer rows.Close()
	result := []Host{}
	for rows.Next() {
		var h Host
		var enabled, spaFallback, browse, redirectKeepPath int
		var providerID sql.NullInt64
		var certID sql.NullInt64
		var headerJSON, allowJSON, denyJSON string
		if err := rows.Scan(&h.ID, &h.Name, &h.Domain, &h.ListenerID, &h.Upstream, &enabled, &h.Version, &h.TLSMode, &h.Path, &h.IconColor, &providerID, &certID, &headerJSON, &h.BasicAuthUser, &h.PasswordHash, &allowJSON, &denyJSON, &h.RateLimitRequests, &h.RateLimitWindow, &h.Type, &h.RootPath, &spaFallback, &browse, &h.RedirectTarget, &h.RedirectStatus, &redirectKeepPath); err != nil {
			fail(w, 500, err)
			return
		}
		h.Enabled = enabled == 1
		h.SpaFallback = spaFallback == 1
		h.Browse = browse == 1
		h.RedirectKeepPath = redirectKeepPath == 1
		if providerID.Valid {
			h.DNSProviderID = &providerID.Int64
		}
		if certID.Valid {
			h.CustomCertificateID = &certID.Int64
		}
		if err := json.Unmarshal([]byte(headerJSON), &h.CustomHeaders); err != nil {
			fail(w, 500, errors.New("stored custom headers are invalid"))
			return
		}
		if err := json.Unmarshal([]byte(allowJSON), &h.IPAllowlist); err != nil {
			fail(w, 500, errors.New("stored IP allowlist is invalid"))
			return
		}
		if err := json.Unmarshal([]byte(denyJSON), &h.IPDenylist); err != nil {
			fail(w, 500, errors.New("stored IP denylist is invalid"))
			return
		}
		h.BasicAuthEnabled = h.PasswordHash != ""
		result = append(result, h)
	}
	if err := rows.Err(); err != nil {
		fail(w, 500, err)
		return
	}
	sources, err := a.certificateSources(r.Context(), result)
	if err != nil {
		fail(w, 500, err)
		return
	}
	for i := range result {
		source := sources[result[i].ID]
		result[i].CertificateSource = &source
	}
	writeJSON(w, 200, result)
}

type Revision struct {
	ID        int64  `json:"id"`
	Status    string `json:"status"`
	Error     string `json:"error,omitempty"`
	CreatedAt string `json:"created_at"`
}

func (a *App) listRevisions(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.QueryContext(r.Context(), `SELECT id,status,error_text,created_at FROM config_revisions ORDER BY id DESC LIMIT 10`)
	if err != nil {
		fail(w, 500, err)
		return
	}
	defer rows.Close()
	out := []Revision{}
	for rows.Next() {
		var v Revision
		if err := rows.Scan(&v.ID, &v.Status, &v.Error, &v.CreatedAt); err != nil {
			fail(w, 500, err)
			return
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		fail(w, 500, err)
		return
	}
	writeJSON(w, 200, out)
}

func (a *App) reapplyConfig(w http.ResponseWriter, r *http.Request) {
	if err := a.apply(r.Context()); err != nil {
		fail(w, http.StatusBadGateway, errors.New("Caddy 仍未接受当前配置，请检查配置历史中的错误"))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) testUpstream(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Upstream string `json:"upstream"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&input); err != nil {
		fail(w, http.StatusBadRequest, errors.New("invalid upstream test request"))
		return
	}
	u, err := parseUpstream(input.Upstream)
	if err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	started := time.Now()
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", net.JoinHostPort(u.Hostname(), u.Port()))
	if err != nil {
		fail(w, http.StatusBadGateway, errors.New("无法连接上游端口，请确认地址正确且目标服务正在运行"))
		return
	}
	_ = conn.Close()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "duration_ms": time.Since(started).Milliseconds()})
}

func (a *App) createHost(w http.ResponseWriter, r *http.Request) {
	var h Host
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&h); err != nil {
		fail(w, 400, errors.New("invalid JSON body"))
		return
	}
	if err := validateHost(&h); err != nil {
		fail(w, 400, err)
		return
	}
	if err := a.validateHostListener(r.Context(), &h); err != nil {
		fail(w, 400, err)
		return
	}
	if err := a.validateTLSSettings(r.Context(), &h); err != nil {
		fail(w, 400, err)
		return
	}
	if err := validateHeaders(h.CustomHeaders); err != nil {
		fail(w, 400, err)
		return
	}
	if err := validateHostSecurity(&h); err != nil {
		fail(w, 400, err)
		return
	}
	if h.BasicAuthEnabled {
		if len(h.BasicAuthPassword) < 8 || len(h.BasicAuthPassword) > 72 {
			fail(w, 400, errors.New("Basic Auth password must be 8-72 bytes"))
			return
		}
		hash, err := hashPassword(h.BasicAuthPassword)
		if err != nil {
			fail(w, 500, errors.New("could not hash Basic Auth password"))
			return
		}
		h.PasswordHash = hash
	}
	headerJSON, _ := json.Marshal(h.CustomHeaders)
	allowJSON, _ := json.Marshal(h.IPAllowlist)
	denyJSON, _ := json.Marshal(h.IPDenylist)
	if err := a.prepareSiteStorage(&h); err != nil {
		fail(w, 400, err)
		return
	}
	res, err := a.db.ExecContext(r.Context(), `INSERT INTO proxy_hosts(name,domain,listener_id,upstream,enabled,tls_mode,path,icon_color,dns_provider_id,custom_certificate_id,custom_headers,basic_auth_user,basic_auth_hash,ip_allowlist,ip_denylist,rate_limit_requests,rate_limit_window,type,root_path,spa_fallback,browse,redirect_target,redirect_status,redirect_keep_path) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, h.Name, h.Domain, h.ListenerID, h.Upstream, boolInt(h.Enabled), h.TLSMode, h.Path, h.IconColor, h.DNSProviderID, h.CustomCertificateID, string(headerJSON), h.BasicAuthUser, h.PasswordHash, string(allowJSON), string(denyJSON), h.RateLimitRequests, h.RateLimitWindow, h.Type, h.RootPath, boolInt(h.SpaFallback), boolInt(h.Browse), h.RedirectTarget, h.RedirectStatus, boolInt(h.RedirectKeepPath))
	if err != nil {
		status, mapped := classifyHostWriteError(err)
		fail(w, status, mapped)
		return
	}
	h.ID, _ = res.LastInsertId()
	if err := a.apply(r.Context()); err != nil {
		slog.Error("Caddy proxy-host apply failed; inspect redacted configuration history")
		fail(w, 502, errors.New("saved but Caddy could not apply the configuration; review configuration history"))
		return
	}
	h.BasicAuthPassword = ""
	h.PasswordHash = ""
	writeJSON(w, 201, h)
}

func (a *App) updateHost(w http.ResponseWriter, r *http.Request) {
	var h Host
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&h); err != nil {
		fail(w, 400, errors.New("invalid JSON body"))
		return
	}
	if err := validateHost(&h); err != nil {
		fail(w, 400, err)
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		fail(w, 400, errors.New("invalid proxy host id"))
		return
	}
	if err := a.validateHostListener(r.Context(), &h); err != nil {
		fail(w, 400, err)
		return
	}
	if err := a.validateTLSSettings(r.Context(), &h); err != nil {
		fail(w, 400, err)
		return
	}
	if err := validateHeaders(h.CustomHeaders); err != nil {
		fail(w, 400, err)
		return
	}
	if err := validateHostSecurity(&h); err != nil {
		fail(w, 400, err)
		return
	}
	headerJSON, _ := json.Marshal(h.CustomHeaders)
	allowJSON, _ := json.Marshal(h.IPAllowlist)
	denyJSON, _ := json.Marshal(h.IPDenylist)
	if err := a.prepareSiteStorage(&h); err != nil {
		fail(w, 400, err)
		return
	}
	var storedHash string
	if h.BasicAuthEnabled {
		if h.BasicAuthPassword != "" {
			if len(h.BasicAuthPassword) < 8 || len(h.BasicAuthPassword) > 72 {
				fail(w, 400, errors.New("Basic Auth password must be 8-72 bytes"))
				return
			}
			storedHash, err = hashPassword(h.BasicAuthPassword)
			if err != nil {
				fail(w, 500, errors.New("could not hash Basic Auth password"))
				return
			}
		} else {
			if err := a.db.QueryRowContext(r.Context(), `SELECT basic_auth_hash FROM proxy_hosts WHERE id=?`, id).Scan(&storedHash); err != nil || storedHash == "" {
				fail(w, 400, errors.New("Basic Auth password is required"))
				return
			}
		}
	}
	res, err := a.db.ExecContext(r.Context(), `UPDATE proxy_hosts SET name=?,domain=?,listener_id=?,upstream=?,enabled=?,tls_mode=?,https_port=443,path=?,icon_color=?,dns_provider_id=?,custom_certificate_id=?,custom_headers=?,basic_auth_user=?,basic_auth_hash=?,ip_allowlist=?,ip_denylist=?,rate_limit_requests=?,rate_limit_window=?,type=?,root_path=?,spa_fallback=?,browse=?,redirect_target=?,redirect_status=?,redirect_keep_path=?,version=version+1,updated_at=CURRENT_TIMESTAMP WHERE id=? AND version=?`, h.Name, h.Domain, h.ListenerID, h.Upstream, boolInt(h.Enabled), h.TLSMode, h.Path, h.IconColor, h.DNSProviderID, h.CustomCertificateID, string(headerJSON), h.BasicAuthUser, storedHash, string(allowJSON), string(denyJSON), h.RateLimitRequests, h.RateLimitWindow, h.Type, h.RootPath, boolInt(h.SpaFallback), boolInt(h.Browse), h.RedirectTarget, h.RedirectStatus, boolInt(h.RedirectKeepPath), id, h.Version)
	if err != nil {
		status, mapped := classifyHostWriteError(err)
		fail(w, status, mapped)
		return
	}
	changed, err := res.RowsAffected()
	if err != nil {
		fail(w, 500, err)
		return
	}
	if changed == 0 {
		var exists int
		if queryErr := a.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM proxy_hosts WHERE id=?`, id).Scan(&exists); queryErr != nil {
			fail(w, 500, queryErr)
			return
		}
		if exists == 0 {
			fail(w, 404, errors.New("proxy host not found"))
			return
		}
		fail(w, 409, errors.New("该站点已被其他会话修改，请刷新后重新编辑"))
		return
	}
	if err := a.apply(r.Context()); err != nil {
		slog.Error("Caddy proxy-host apply failed; inspect redacted configuration history")
		fail(w, 502, errors.New("saved but Caddy could not apply the configuration; review configuration history"))
		return
	}
	h.BasicAuthPassword = ""
	h.PasswordHash = ""
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (a *App) deleteHost(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		fail(w, 400, errors.New("invalid proxy host id"))
		return
	}
	res, err := a.db.ExecContext(r.Context(), `DELETE FROM proxy_hosts WHERE id=?`, id)
	if err != nil {
		fail(w, 500, err)
		return
	}
	changed, err := res.RowsAffected()
	if err != nil || changed == 0 {
		fail(w, 404, errors.New("proxy host not found"))
		return
	}
	if err := a.apply(r.Context()); err != nil {
		slog.Error("Caddy proxy-host apply failed; inspect redacted configuration history")
		fail(w, 502, errors.New("deleted but Caddy could not apply the configuration; review configuration history"))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// validateStaticRoot checks a static site root: it must be an absolute path
// with no parent-directory segments. The directory itself may not exist yet;
// prepareSiteStorage creates it when the panel manages it.
func validateStaticRoot(root string) error {
	root = strings.TrimSpace(root)
	if root == "" {
		return errors.New("static site root directory is required")
	}
	if !filepath.IsAbs(root) {
		return errors.New("static site root must be an absolute path")
	}
	if strings.ContainsRune(root, 0) {
		return errors.New("static site root contains invalid characters")
	}
	for _, segment := range strings.FieldsFunc(root, func(r rune) bool { return r == '/' || r == '\\' }) {
		if segment == ".." {
			return errors.New("static site root cannot contain .. segments")
		}
	}
	return nil
}

// managedSitesDir is the only tree the panel may write into (the systemd unit
// gives the service write access to its data directory alone). File uploads
// are accepted only for static sites rooted here.
func (a *App) managedSitesDir() string {
	return filepath.Join(a.dataDir, "sites")
}

// prepareSiteStorage resolves a static site's root and makes sure it exists.
// Roots inside the managed tree are created on demand; external roots must
// already exist because the service cannot write outside its data directory.
func (a *App) prepareSiteStorage(h *Host) error {
	if h.Type != "static" {
		return nil
	}
	root := strings.TrimSpace(h.RootPath)
	if root == "" {
		slug := strings.ToLower(h.Domain)
		var b strings.Builder
		for _, r := range slug {
			if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '.' {
				b.WriteRune(r)
			} else {
				b.WriteByte('-')
			}
		}
		name := strings.Trim(b.String(), "-.")
		if name == "" {
			name = "site"
		}
		root = filepath.Join(a.managedSitesDir(), name)
		h.RootPath = root
	}
	if err := validateStaticRoot(root); err != nil {
		return err
	}
	h.RootPath = filepath.Clean(root)
	if err := os.MkdirAll(h.RootPath, 0750); err != nil {
		return errors.New("static site root is not accessible; create the directory on the server first")
	}
	info, err := os.Stat(h.RootPath)
	if err != nil || !info.IsDir() {
		return errors.New("static site root must be an existing directory")
	}
	return nil
}

func validateHost(h *Host) error {
	h.Name = strings.TrimSpace(h.Name)
	if len([]rune(h.Name)) > 100 {
		return errors.New("站点名称不能超过 100 个字符")
	}
	h.Domain = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(h.Domain)), ".")
	if h.Domain == "" || !domainRE.MatchString(h.Domain) {
		return errors.New("domain must be a valid hostname")
	}
	switch h.Type {
	case "", "proxy":
		h.Type = "proxy"
		u, err := parseUpstream(h.Upstream)
		if err != nil {
			return err
		}
		h.Upstream = u.String()
	case "static":
		h.Upstream = ""
		h.RedirectTarget = ""
		h.RedirectStatus = 0
		// An empty root is fine here: prepareSiteStorage assigns a managed
		// directory below. Anything non-empty must be a clean absolute path.
		if strings.TrimSpace(h.RootPath) != "" {
			if err := validateStaticRoot(h.RootPath); err != nil {
				return err
			}
		}
	case "redirect":
		h.Upstream = ""
		h.RootPath = ""
		h.SpaFallback = false
		h.Browse = false
		target := strings.TrimSpace(h.RedirectTarget)
		if target == "" {
			return errors.New("redirect target is required")
		}
		u, err := url.Parse(target)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return errors.New("redirect target must be an http(s):// URL")
		}
		if u.RawQuery != "" || u.Fragment != "" {
			return errors.New("redirect target cannot contain a query string or fragment; the request's own is preserved")
		}
		h.RedirectTarget = strings.TrimRight(target, "/")
		switch h.RedirectStatus {
		case 0:
			h.RedirectStatus = 301
		case 301, 302, 307, 308:
		default:
			return errors.New("redirect status must be 301, 302, 307 or 308")
		}
	default:
		return errors.New("site type must be proxy, static or redirect")
	}
	h.Path = strings.TrimSpace(h.Path)
	if h.Path != "" {
		if !strings.HasPrefix(h.Path, "/") || len(h.Path) > 256 || strings.ContainsAny(h.Path, "?#*\\\r\n\x00\t ") {
			return errors.New("访问路径须以 / 开始，不能包含空格、通配符、查询参数或片段")
		}
		h.Path = strings.TrimRight(h.Path, "/")
	}
	if h.Path == "/" {
		h.Path = ""
	}
	switch h.IconColor {
	case "", "blue":
		h.IconColor = "blue"
	case "teal", "green", "lime", "amber", "orange", "rose", "pink", "violet", "slate":
	default:
		return errors.New("无效的站点图标颜色")
	}
	if h.TLSMode == "" {
		h.TLSMode = "auto"
	}
	if h.CustomHeaders == nil {
		h.CustomHeaders = map[string]string{}
	}
	return nil
}

func parseUpstream(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return nil, errors.New("上游地址必须是 HTTP(S) 地址，例如 https://example.com")
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("上游地址不能包含用户名、密码、查询参数或片段")
	}
	if strings.HasSuffix(u.Host, ":") {
		return nil, errors.New("上游地址的端口不能为空，请删除末尾冒号或填写端口")
	}
	if strings.Contains(u.Hostname(), ":") && !strings.HasPrefix(u.Host, "[") {
		return nil, errors.New("IPv6 上游地址必须使用方括号，例如 http://[::1]")
	}
	if u.Port() == "" {
		port := "80"
		if u.Scheme == "https" {
			port = "443"
		}
		u.Host = net.JoinHostPort(u.Hostname(), port)
	}
	for _, digit := range u.Port() {
		if digit < '0' || digit > '9' {
			return nil, errors.New("上游端口必须是数字")
		}
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil || port < 1 || port > 65535 {
		return nil, errors.New("上游端口范围必须是 1 到 65535")
	}
	if u.Path != "" && u.Path != "/" {
		return nil, errors.New("暂不支持在上游地址中填写路径前缀")
	}
	return u, nil
}

func validateHeaders(headers map[string]string) error {
	if len(headers) > 40 {
		return errors.New("at most 40 custom request headers are allowed")
	}
	for key, value := range headers {
		if !headerNameRE.MatchString(key) || strings.EqualFold(key, "Host") || strings.EqualFold(key, "Content-Length") || strings.EqualFold(key, "Connection") || strings.EqualFold(key, "Transfer-Encoding") || strings.EqualFold(key, "Upgrade") {
			return fmt.Errorf("invalid or reserved custom header name %q", key)
		}
		if len(value) > 4096 || strings.ContainsAny(value, "\r\n\x00") {
			return fmt.Errorf("invalid value for custom header %q", key)
		}
	}
	return nil
}

func validateHostSecurity(h *Host) error {
	if h.BasicAuthEnabled {
		h.BasicAuthUser = strings.TrimSpace(h.BasicAuthUser)
		if h.BasicAuthUser == "" || len(h.BasicAuthUser) > 128 || strings.ContainsAny(h.BasicAuthUser, ":\r\n\x00") {
			return errors.New("Basic Auth username must be 1-128 characters and cannot contain colon or control characters")
		}
	} else {
		h.BasicAuthUser = ""
		h.BasicAuthPassword = ""
	}
	var err error
	h.IPAllowlist, err = normalizeCIDRs(h.IPAllowlist)
	if err != nil {
		return fmt.Errorf("invalid IP allowlist: %w", err)
	}
	h.IPDenylist, err = normalizeCIDRs(h.IPDenylist)
	if err != nil {
		return fmt.Errorf("invalid IP denylist: %w", err)
	}
	if h.RateLimitRequests < 0 || h.RateLimitRequests > 1000000 {
		return errors.New("rate limit must be between 0 and 1000000 requests per window")
	}
	if h.RateLimitRequests == 0 {
		h.RateLimitWindow = ""
		return nil
	}
	if h.RateLimitWindow == "" {
		h.RateLimitWindow = "1s"
	}
	duration, err := time.ParseDuration(h.RateLimitWindow)
	if err != nil || duration < 100*time.Millisecond || duration > 24*time.Hour {
		return errors.New("rate limit window must be between 100ms and 24h")
	}
	return nil
}

func normalizeCIDRs(input []string) ([]string, error) {
	if len(input) > 1000 {
		return nil, errors.New("at most 1000 IP ranges are allowed")
	}
	result := make([]string, 0, len(input))
	seen := map[string]bool{}
	for _, raw := range input {
		value := strings.TrimSpace(raw)
		if value == "" {
			continue
		}
		if ip := net.ParseIP(value); ip != nil {
			if ip.To4() != nil {
				value += "/32"
			} else {
				value += "/128"
			}
		}
		_, network, err := net.ParseCIDR(value)
		if err != nil {
			return nil, errors.New("entries must be IP addresses or CIDR ranges")
		}
		normalized := network.String()
		if !seen[normalized] {
			seen[normalized] = true
			result = append(result, normalized)
		}
	}
	return result, nil
}

func (a *App) validateTLSSettings(ctx context.Context, h *Host) error {
	var managed int
	if err := a.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM managed_certificates WHERE domain=?`, h.Domain).Scan(&managed); err != nil {
		return err
	}
	if managed > 0 && h.TLSMode != "auto" && h.TLSMode != "none" {
		return errors.New("该域名已配置托管证书，请选择自动 HTTPS 以使用并自动续期")
	}
	if strings.HasPrefix(h.Domain, "*.") && ((h.TLSMode == "auto" && managed == 0) || h.TLSMode == "http-01") {
		return errors.New("泛域名不能使用自动 HTTPS 或 HTTP-01，请选择 DNS-01、内部 CA 或匹配的自定义证书")
	}
	switch h.TLSMode {
	case "auto", "http-01", "none", "internal":
		if h.DNSProviderID != nil {
			return errors.New("DNS provider can only be selected for DNS-01 TLS")
		}
	case "dns-01":
		if h.DNSProviderID == nil || *h.DNSProviderID < 1 {
			return errors.New("a DNS provider is required for DNS-01 TLS")
		}
		var exists int
		if err := a.db.QueryRowContext(ctx, `SELECT 1 FROM dns_providers WHERE id=?`, *h.DNSProviderID).Scan(&exists); err != nil {
			return errors.New("selected DNS provider does not exist")
		}
	case "custom":
		if h.DNSProviderID != nil {
			return errors.New("DNS provider cannot be selected for a custom certificate")
		}
		if h.CustomCertificateID == nil || *h.CustomCertificateID < 1 {
			return errors.New("a custom certificate is required")
		}
		var namesJSON string
		if err := a.db.QueryRowContext(ctx, `SELECT dns_names_json FROM custom_certificates WHERE id=?`, *h.CustomCertificateID).Scan(&namesJSON); err != nil {
			return errors.New("selected custom certificate does not exist")
		}
		var names []string
		if json.Unmarshal([]byte(namesJSON), &names) != nil || !certificateMatchesDomain(names, h.Domain) {
			return errors.New("custom certificate SAN does not cover this domain")
		}
	default:
		return errors.New("TLS mode must be auto, http-01, dns-01, internal, custom, or none")
	}
	return nil
}

func hostUsesInternalCA(h Host) bool {
	if h.TLSMode == "internal" {
		return true
	}
	if h.TLSMode != "auto" {
		return false
	}
	name := strings.TrimPrefix(strings.ToLower(h.Domain), "*.")
	return name == "localhost" || strings.HasSuffix(name, ".localhost") || net.ParseIP(name) != nil
}

func (a *App) apply(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.applyLocked(ctx)
}

// scheduleApply coalesces bursty configuration changes, notably automatic IP
// bans, into at most one Caddy validate + load per window. Without it every
// banned request could fork a Caddy subprocess.
func (a *App) scheduleApply() {
	select {
	case a.applyKick <- struct{}{}:
	default:
	}
}

func (a *App) applyWorker() {
	for {
		select {
		case <-a.ctx.Done():
			return
		case <-a.applyKick:
			timer := time.NewTimer(500 * time.Millisecond)
			select {
			case <-a.ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
			// Drop pending kicks; the apply below already covers them.
			select {
			case <-a.applyKick:
			default:
			}
			ctx, cancel := context.WithTimeout(a.ctx, 20*time.Second)
			if err := a.apply(ctx); err != nil && a.ctx.Err() == nil {
				// Bans stay in the database; the next change or restart retries.
				slog.Warn("deferred Caddy apply failed", "error", err)
			}
			cancel()
		}
	}
}

// applyLocked requires a.mu; listener changes hold it across save, reload and recovery.
func (a *App) applyLocked(ctx context.Context) error {
	rows, err := a.db.QueryContext(ctx, `SELECT id,domain,listener_id,upstream,tls_mode,path,icon_color,dns_provider_id,custom_certificate_id,custom_headers,basic_auth_user,basic_auth_hash,ip_allowlist,ip_denylist,rate_limit_requests,rate_limit_window,type,root_path,spa_fallback,browse,redirect_target,redirect_status,redirect_keep_path FROM proxy_hosts WHERE enabled=1 ORDER BY domain`)
	if err != nil {
		return err
	}
	hosts := []Host{}
	for rows.Next() {
		var h Host
		var providerID sql.NullInt64
		var certID sql.NullInt64
		var spaFallback, browse, redirectKeepPath int
		var headers, allowJSON, denyJSON string
		if err := rows.Scan(&h.ID, &h.Domain, &h.ListenerID, &h.Upstream, &h.TLSMode, &h.Path, &h.IconColor, &providerID, &certID, &headers, &h.BasicAuthUser, &h.PasswordHash, &allowJSON, &denyJSON, &h.RateLimitRequests, &h.RateLimitWindow, &h.Type, &h.RootPath, &spaFallback, &browse, &h.RedirectTarget, &h.RedirectStatus, &redirectKeepPath); err != nil {
			rows.Close()
			return err
		}
		h.SpaFallback = spaFallback == 1
		h.Browse = browse == 1
		h.RedirectKeepPath = redirectKeepPath == 1
		if providerID.Valid {
			h.DNSProviderID = &providerID.Int64
		}
		if certID.Valid {
			h.CustomCertificateID = &certID.Int64
		}
		if err := json.Unmarshal([]byte(headers), &h.CustomHeaders); err != nil {
			rows.Close()
			return errors.New("stored custom headers are invalid")
		}
		if err := json.Unmarshal([]byte(allowJSON), &h.IPAllowlist); err != nil {
			rows.Close()
			return errors.New("stored IP allowlist is invalid")
		}
		if err := json.Unmarshal([]byte(denyJSON), &h.IPDenylist); err != nil {
			rows.Close()
			return errors.New("stored IP denylist is invalid")
		}
		hosts = append(hosts, h)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	routesByListener := map[string][]any{}
	policies, managedDomains, managedInternal, err := a.managedPolicies(ctx)
	if err != nil {
		return err
	}
	noTLSHosts := []string{}
	usesInternalTLS := managedInternal
	// Ban rules are a security policy: if they cannot be read, refuse to push a
	// configuration that silently drops every rule. Capture all errors instead
	// of continuing with an empty map.
	now := time.Now().Unix()
	if _, err := a.db.ExecContext(ctx, `DELETE FROM ip_bans WHERE expires_at>0 AND expires_at<=?`, now); err != nil {
		return err
	}
	banRows, err := a.db.QueryContext(ctx, `SELECT domain,ip FROM ip_bans WHERE expires_at=0 OR expires_at>? ORDER BY domain,ip`, now)
	if err != nil {
		return err
	}
	bansByDomain := map[string][]string{}
	for banRows.Next() {
		var d, ip string
		if err := banRows.Scan(&d, &ip); err != nil {
			banRows.Close()
			return err
		}
		bansByDomain[d] = append(bansByDomain[d], ip)
	}
	banRows.Close()
	if err := banRows.Err(); err != nil {
		return err
	}
	for _, host := range hosts {
		if host.TLSMode == "none" {
			noTLSHosts = append(noTLSHosts, host.Domain)
		}
		hostRoutes := []any{}
		hostMatcher := map[string]any{"host": []string{host.Domain}}
		if host.Path != "" {
			hostMatcher["path"] = []string{host.Path, host.Path + "/*"}
		}
		handlers := []any{}
		if len(host.IPDenylist) > 0 {
			hostRoutes = append(hostRoutes, map[string]any{"match": []any{map[string]any{"host": []string{host.Domain}, "remote_ip": map[string]any{"ranges": host.IPDenylist}}}, "handle": []any{map[string]any{"handler": "static_response", "status_code": 403, "body": "Forbidden"}}})
		}
		if active := bansByDomain[host.Domain]; len(active) > 0 {
			ranges := []string{}
			for _, ip := range active {
				if ip == "foreign" {
					hostRoutes = append(hostRoutes, map[string]any{"match": []any{map[string]any{"host": []string{host.Domain}, "not": []any{map[string]any{"remote_ip": map[string]any{"ranges": chinaAndPrivateRanges()}}}}}, "handle": []any{map[string]any{"handler": "static_response", "status_code": 403, "body": "Region access denied"}}})
				} else {
					ranges = append(ranges, ip)
				}
			}
			active = ranges
			if len(active) > 0 {
				hostRoutes = append(hostRoutes, map[string]any{"match": []any{map[string]any{"host": []string{host.Domain}, "remote_ip": map[string]any{"ranges": active}}}, "handle": []any{map[string]any{"handler": "static_response", "status_code": 403, "body": "Temporarily blocked"}}})
			}
		}
		if len(host.IPAllowlist) > 0 {
			hostRoutes = append(hostRoutes, map[string]any{"match": []any{map[string]any{"host": []string{host.Domain}, "not": []any{map[string]any{"remote_ip": map[string]any{"ranges": host.IPAllowlist}}}}}, "handle": []any{map[string]any{"handler": "static_response", "status_code": 403, "body": "Forbidden"}}})
		}
		if host.PasswordHash != "" {
			handlers = append(handlers, map[string]any{"handler": "authentication", "providers": map[string]any{"http_basic": map[string]any{"accounts": []any{map[string]any{"username": host.BasicAuthUser, "password": host.PasswordHash}}}}})
		}
		if host.RateLimitRequests > 0 {
			zoneName := fmt.Sprintf("proxy_host_%d", host.ID)
			handlers = append(handlers, map[string]any{"handler": "rate_limit", "rate_limits": map[string]any{zoneName: map[string]any{"key": "{http.request.remote.host}", "window": host.RateLimitWindow, "max_events": host.RateLimitRequests}}})
		}
		if len(host.CustomHeaders) > 0 {
			setHeaders := map[string][]string{}
			for key, value := range host.CustomHeaders {
				setHeaders[key] = []string{value}
			}
			handlers = append(handlers, map[string]any{"handler": "headers", "request": map[string]any{"set": setHeaders}})
		}
		switch host.Type {
		case "static":
			// Caddyfile's `root * <dir>` compiles into a vars handler; both the
			// file matcher (SPA fallback) and file_server resolve paths from it.
			rootMatcher := map[string]any{"host": []string{host.Domain}}
			if host.Path != "" {
				rootMatcher["path"] = []string{host.Path, host.Path + "/*"}
			}
			hostRoutes = append(hostRoutes, map[string]any{"match": []any{rootMatcher}, "handle": []any{map[string]any{"handler": "vars", "root": host.RootPath}}})
			if host.SpaFallback {
				// Client-side routers need unknown paths to fall back to
				// index.html. This is a separate route ahead of the file server:
				// Caddy's try_files lives in the file matcher, not a handler.
				spaMatcher := map[string]any{"host": []string{host.Domain}, "file": map[string]any{"try_files": []string{"{http.request.uri.path}", "/index.html"}}}
				if host.Path != "" {
					spaMatcher["path"] = []string{host.Path, host.Path + "/*"}
				}
				hostRoutes = append(hostRoutes, map[string]any{"match": []any{spaMatcher}, "handle": []any{map[string]any{"handler": "rewrite", "uri": "{http.matchers.file.relative}"}}})
			}
			fileServer := map[string]any{"handler": "file_server", "root": host.RootPath}
			if host.Browse {
				fileServer["browse"] = map[string]any{}
			}
			handlers = append(handlers, fileServer)
		case "redirect":
			location := host.RedirectTarget
			if host.RedirectKeepPath {
				location += "{http.request.uri}"
			}
			handlers = append(handlers, map[string]any{"handler": "static_response", "status_code": host.RedirectStatus, "headers": map[string]any{"Location": []string{location}}})
		default:
			proxy := map[string]any{"handler": "reverse_proxy", "upstreams": []any{map[string]any{"dial": upstreamDial(host.Upstream)}}}
			if upstream, parseErr := url.Parse(host.Upstream); parseErr == nil && upstream.Scheme == "https" {
				proxy["transport"] = map[string]any{"protocol": "http", "tls": map[string]any{"server_name": upstream.Hostname()}}
			}
			handlers = append(handlers, proxy)
		}
		hostRoutes = append(hostRoutes, map[string]any{"match": []any{hostMatcher}, "handle": handlers})
		routesByListener[host.ListenerID] = append(routesByListener[host.ListenerID], hostRoutes...)
		if host.TLSMode == "http-01" {
			policies = append(policies, map[string]any{"subjects": []string{host.Domain}, "issuers": []any{map[string]any{"module": "acme", "challenges": map[string]any{"http": map[string]any{}}}}})
		}
		if hostUsesInternalCA(host) {
			usesInternalTLS = true
		}
		if host.TLSMode == "internal" {
			policies = append(policies, map[string]any{"subjects": []string{host.Domain}, "issuers": []any{map[string]any{"module": "internal"}}})
		}
		if host.TLSMode == "dns-01" {
			if host.DNSProviderID == nil {
				return errors.New("DNS provider is missing for a DNS-01 host")
			}
			providerType, credentials, err := a.loadDNSProvider(ctx, *host.DNSProviderID)
			if err != nil {
				return err
			}
			provider := map[string]any{"name": providerType}
			for key, value := range credentials {
				provider[key] = value
			}
			policies = append(policies, map[string]any{"subjects": []string{host.Domain}, "issuers": []any{map[string]any{"module": "acme", "challenges": map[string]any{"dns": map[string]any{"provider": provider}}}}})
		}
	}
	listeners, err := a.webListeners(ctx)
	if err != nil {
		return err
	}
	servers := map[string]any{}
	// Each site must redirect to the HTTPS port of the listener it is attached
	// to; using one global "primary" port sent sites on :8443 to :443, where
	// they fall through to the 404 handler.
	httpsPorts := map[string]int{}
	primaryTLS := 0
	for _, listener := range listeners {
		if listener.Protocol != "https" {
			continue
		}
		httpsPorts[listener.ID] = listener.Port
		if primaryTLS == 0 {
			primaryTLS = listener.Port
		}
	}
	for _, listener := range listeners {
		serverRoutes := append([]any{}, routesByListener[listener.ID]...)
		server := map[string]any{"listen": []string{fmt.Sprintf(":%d", listener.Port)}, "logs": map[string]any{"default_logger_name": "viya_access_" + listener.ID, "should_log_credentials": false}}
		if listener.Protocol == "https" {
			server["tls_connection_policies"] = []any{map[string]any{}}
			server["automatic_https"] = map[string]any{"skip": noTLSHosts, "disable_redirects": true}
		} else {
			redirects := []any{}
			for _, host := range hosts {
				if host.TLSMode == "none" {
					continue
				}
				port := httpsPorts[host.ListenerID]
				if port == 0 {
					port = primaryTLS
				}
				if port == 0 {
					continue
				}
				address := "{http.request.host}"
				if port != 443 {
					address = net.JoinHostPort(address, strconv.Itoa(port))
				}
				redirects = append(redirects, map[string]any{"match": []any{map[string]any{"host": []string{host.Domain}}}, "handle": []any{map[string]any{"handler": "static_response", "status_code": 308, "headers": map[string][]string{"Location": {"https://" + address + "{http.request.uri}"}}}}})
			}
			serverRoutes = append(serverRoutes, redirects...)
			server["automatic_https"] = map[string]any{"disable": true}
		}
		// Unmatched and disabled sites must not look like a healthy empty response.
		serverRoutes = append(serverRoutes, map[string]any{"handle": []any{map[string]any{"handler": "static_response", "status_code": 404, "body": "Site unavailable"}}})
		server["routes"] = serverRoutes
		servers[fmt.Sprintf("viya_%s_%d", listener.Protocol, listener.Port)] = server
	}
	apps := map[string]any{"http": map[string]any{"servers": servers}}
	if usesInternalTLS {
		// Viya runs Caddy as an unprivileged service. Internal CA roots must be
		// distributed and trusted explicitly by the operator, so do not ask Caddy
		// to invoke sudo or modify the host's trust stores during config reloads.
		apps["pki"] = map[string]any{"certificate_authorities": map[string]any{"local": map[string]any{"install_trust": false}}}
	}
	l4Rows, err := a.db.QueryContext(ctx, `SELECT id,name,listen_address,listen_port,upstream,protocol FROM layer4_rules WHERE enabled=1 ORDER BY listen_port,id`)
	if err != nil {
		return err
	}
	l4Servers := map[string]any{}
	for l4Rows.Next() {
		var rule Layer4Rule
		if err := l4Rows.Scan(&rule.ID, &rule.Name, &rule.ListenAddress, &rule.ListenPort, &rule.Upstream, &rule.Protocol); err != nil {
			l4Rows.Close()
			return err
		}
		for _, protocol := range l4Protocols(rule.Protocol) {
			serverName := fmt.Sprintf("viya_%s_%d", protocol, rule.ID)
			listen := fmt.Sprintf("%s/%s", protocol, net.JoinHostPort(rule.ListenAddress, strconv.Itoa(rule.ListenPort)))
			l4Servers[serverName] = map[string]any{"listen": []string{listen}, "routes": []any{map[string]any{"handle": []any{map[string]any{"handler": "proxy", "upstreams": []any{map[string]any{"dial": []string{l4Dial(protocol, rule.Upstream)}}}}}}}}
		}
	}
	if err := l4Rows.Err(); err != nil {
		l4Rows.Close()
		return err
	}
	l4Rows.Close()
	if len(l4Servers) > 0 {
		apps["layer4"] = map[string]any{"servers": l4Servers}
	}
	tlsApp := map[string]any{}
	if len(policies) > 0 {
		tlsApp["automation"] = map[string]any{"policies": policies}
	}
	certificates := []any{}
	seenCerts := map[int64]bool{}
	for _, host := range hosts {
		if host.TLSMode != "custom" || host.CustomCertificateID == nil || seenCerts[*host.CustomCertificateID] {
			continue
		}
		var certPath, keyPath string
		if err := a.db.QueryRowContext(ctx, `SELECT cert_path,key_path FROM custom_certificates WHERE id=?`, *host.CustomCertificateID).Scan(&certPath, &keyPath); err != nil {
			return errors.New("custom certificate files are unavailable")
		}
		if certPath == "" || keyPath == "" {
			return fmt.Errorf("custom certificate %d has no stored file paths; remove or re-upload it", *host.CustomCertificateID)
		}
		certificates = append(certificates, map[string]any{"certificate": certPath, "key": keyPath})
		seenCerts[*host.CustomCertificateID] = true
	}
	if len(certificates) > 0 {
		tlsApp["certificates"] = map[string]any{"load_files": certificates}
	}
	if len(managedDomains) > 0 {
		loaders, ok := tlsApp["certificates"].(map[string]any)
		if !ok {
			loaders = map[string]any{}
			tlsApp["certificates"] = loaders
		}
		loaders["automate"] = managedDomains
	}
	if len(tlsApp) > 0 {
		apps["tls"] = tlsApp
	}
	config := map[string]any{"admin": map[string]any{"listen": a.adminListen, "config": map[string]any{"persist": false}}, "apps": apps}
	logConfig, logErr := a.loadLogSettings()
	if logErr != nil {
		return logErr
	}
	config["logging"] = map[string]any{"logs": map[string]any{"default": map[string]any{"level": logConfig.Level}, "viya_access": map[string]any{"level": "INFO", "writer": map[string]any{"output": "file", "filename": filepath.Join(a.dataDir, "access.log"), "roll_size_mb": logConfig.AccessMB, "roll_keep": logConfig.Backups, "roll_keep_days": logConfig.Days, "roll_compression": "gzip"}, "encoder": map[string]any{"format": "json"}}, "layer4": map[string]any{"level": "DEBUG", "writer": map[string]any{"output": "file", "filename": filepath.Join(a.dataDir, "l4-access.log"), "roll_size_mb": logConfig.AccessMB, "roll_keep": logConfig.Backups, "roll_keep_days": logConfig.Days, "roll_compression": "gzip"}, "encoder": map[string]any{"format": "json"}}}}
	body, err := json.Marshal(config)
	if err != nil {
		return err
	}
	configFile, err := os.CreateTemp("", "viya-caddy-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(configFile.Name())
	if err := configFile.Chmod(0600); err != nil {
		configFile.Close()
		return err
	}
	if _, err := configFile.Write(body); err != nil {
		configFile.Close()
		return err
	}
	if err := configFile.Close(); err != nil {
		return err
	}
	validate := exec.CommandContext(ctx, a.caddyBin, "validate", "--config", configFile.Name())
	if output, err := validate.CombinedOutput(); err != nil {
		cause := fmt.Errorf("caddy validate: %s: %w", strings.TrimSpace(string(output)), err)
		a.recordRevision(ctx, body, "failed", cause)
		return cause
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(a.adminURL, "/")+"/load", strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	client := http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		a.recordRevision(ctx, body, "failed", err)
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		cause := fmt.Errorf("Caddy returned %s", resp.Status)
		a.recordRevision(ctx, body, "failed", cause)
		return cause
	}
	a.recordRevision(ctx, body, "applied", nil)
	return nil
}

func (a *App) recordRevision(ctx context.Context, config []byte, status string, cause error) {
	errText := ""
	if cause != nil {
		errText = cause.Error()
	}
	redacted, secrets := redactConfig(config)
	for _, secret := range secrets {
		errText = strings.ReplaceAll(errText, secret, "[redacted]")
	}
	if _, err := a.db.ExecContext(ctx, `INSERT INTO config_revisions(status,config_json,error_text) VALUES(?,?,?)`, status, string(redacted), errText); err != nil {
		slog.Error("record config revision", "error", err)
		return
	}
	// The UI only ever shows the newest 10 entries; keep a bounded history so
	// repeated applies cannot grow the database without limit.
	if _, err := a.db.ExecContext(ctx, `DELETE FROM config_revisions WHERE id NOT IN (SELECT id FROM config_revisions ORDER BY id DESC LIMIT 50)`); err != nil {
		slog.Warn("prune config revisions", "error", err)
	}
}

func redactConfig(config []byte) ([]byte, []string) {
	var value any
	if json.Unmarshal(config, &value) != nil {
		return []byte(`{"redacted":true}`), nil
	}
	secrets := []string{}
	var visit func(any, string)
	visit = func(current any, key string) {
		switch item := current.(type) {
		case map[string]any:
			for childKey, child := range item {
				if isSecretField(childKey) {
					if text, ok := child.(string); ok && text != "" {
						secrets = append(secrets, text)
					}
					item[childKey] = "[redacted]"
				} else {
					visit(child, childKey)
				}
			}
		case []any:
			for _, child := range item {
				visit(child, key)
			}
		}
	}
	visit(value, "")
	redacted, err := json.Marshal(value)
	if err != nil {
		return []byte(`{"redacted":true}`), secrets
	}
	return redacted, secrets
}

func isSecretField(key string) bool {
	lower := strings.ToLower(key)
	return strings.Contains(lower, "secret") || strings.Contains(lower, "token") || strings.Contains(lower, "password") || strings.Contains(lower, "access_key")
}

func upstreamDial(raw string) string { u, _ := url.Parse(raw); return u.Host }
func caddyAdminListen(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" || u.User != nil || u.Path != "" && u.Path != "/" || u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("Caddy Admin API must be a local HTTP URL with a host and port")
	}
	host, port, err := net.SplitHostPort(u.Host)
	if err != nil || port == "" {
		return "", errors.New("Caddy Admin API must include a port")
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		return "", errors.New("Caddy Admin API port is invalid")
	}
	ip := net.ParseIP(host)
	if !(ip != nil && ip.IsLoopback() || strings.EqualFold(host, "localhost")) {
		return "", errors.New("Caddy Admin API must use a loopback address")
	}
	return net.JoinHostPort(host, port), nil
}
func validateListenAddress(address string) error {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return errors.New("listener must be an IP address and explicit port, such as :8080")
	}
	if host != "" {
		ip := net.ParseIP(host)
		if ip == nil {
			return errors.New("listener host must be an IP address or empty")
		}
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		return errors.New("listener port is invalid")
	}
	return nil
}
func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
func ensureColumn(db *sql.DB, table, column, definition string) error {
	rows, err := db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, dataType string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &primaryKey); err != nil {
			return err
		}
		if name == column {
			return rows.Err()
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	_, err = db.Exec(`ALTER TABLE ` + table + ` ADD COLUMN ` + column + ` ` + definition)
	return err
}
func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// classifyHostWriteError turns SQLite constraint failures into actionable
// messages instead of reporting every failure as a duplicate domain.
func classifyHostWriteError(err error) (int, error) {
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "unique constraint failed: proxy_hosts.domain"):
		return http.StatusConflict, errors.New("该域名已被其他站点占用，请修改域名或改用其他端口监听")
	case strings.Contains(message, "foreign key constraint failed"):
		return http.StatusBadRequest, errors.New("所选的端口监听、DNS 凭据或自定义证书已不存在，请重新选择")
	case strings.Contains(message, "not null constraint failed"):
		return http.StatusBadRequest, errors.New("站点信息不完整，请检查必填字段")
	case strings.Contains(message, "check constraint failed"):
		return http.StatusBadRequest, errors.New("站点字段取值不被允许，请检查 TLS 模式与限流设置")
	}
	slog.Error("proxy host write failed", "error", err)
	return http.StatusInternalServerError, errors.New("站点保存失败，请稍后重试；详细原因已写入服务日志")
}

func fail(w http.ResponseWriter, status int, err error) {
	if status >= 500 {
		slog.Error("request failed", "status", status, "error", err)
	}
	// Engine, database and filesystem errors carry table names, columns,
	// absolute paths or Caddy config fragments; never echo them verbatim.
	// 502/504 messages from upstream calls stay readable: they are actionable.
	if status == http.StatusInternalServerError || status == http.StatusServiceUnavailable {
		if internalFailure(err) {
			writeJSON(w, status, map[string]string{"error": "服务器内部错误，请稍后重试；详细原因已写入服务日志"})
			return
		}
	}
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

func internalFailure(err error) bool {
	var sqliteErr *sqlite.Error
	if errors.As(err, &sqliteErr) {
		return true
	}
	var pathErr *os.PathError
	if errors.As(err, &pathErr) {
		return true
	}
	var linkErr *os.LinkError
	if errors.As(err, &linkErr) {
		return true
	}
	var syscallErr *os.SyscallError
	if errors.As(err, &syscallErr) {
		return true
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return true
	}
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return true
	}
	message := err.Error()
	return strings.HasPrefix(message, "sql: ") || strings.HasPrefix(message, "caddy validate:") || strings.Contains(message, "modernc.org/sqlite")
}
func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		slog.Debug("request", "method", r.Method, "path", r.URL.Path, "duration", time.Since(start))
	})
}
