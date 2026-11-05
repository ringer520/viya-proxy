package main

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"viya/internal/secretstore"
)

// SecurityHeaders is the response-header policy injected for a site.
//
// Inherit marks a site that keeps following its parent policy, so a
// "apply to existing sites" run knows which rows it may overwrite. When
// Enabled is false the site carries no policy at all.
type SecurityHeaders struct {
	Mode           string `json:"mode,omitempty"`
	Enabled        bool   `json:"enabled"`
	Inherit        bool   `json:"inherit"`
	FrameOptions   string `json:"frame_options,omitempty"`
	NoSniff        bool   `json:"nosniff"`
	HSTS           bool   `json:"hsts"`
	ReferrerPolicy string `json:"referrer_policy,omitempty"`
	CSP            string `json:"csp,omitempty"`
}

type Host struct {
	PolicySource        string             `json:"policy_source,omitempty"`
	ID                  int64              `json:"id"`
	Name                string             `json:"name"`
	Tags                []string           `json:"tags"`
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
	// Static sites: fall back to index.html for client-side routing, and let
	// visitors browse the directory listing. File-service sites use Browse as
	// their web directory listing switch.
	SpaFallback bool `json:"spa_fallback"`
	Browse      bool `json:"browse"`
	// File-service sites: expose the same share over WebDAV, optionally writable.
	WebDAV           bool            `json:"webdav"`
	WebDAVWrite      bool            `json:"webdav_write"`
	RedirectTarget   string          `json:"redirect_target,omitempty"`
	RedirectStatus   int             `json:"redirect_status,omitempty"`
	RedirectKeepPath bool            `json:"redirect_keep_path"`
	SecurityHeaders  SecurityHeaders `json:"security_headers"`
	MaxBodyBytes     int64           `json:"max_body_bytes"`
	AllowedMethods   []string        `json:"allowed_methods"`
	HideServer       bool            `json:"hide_server"`
	// Path rewriting: strip the matched Path prefix before forwarding, and/or
	// rewrite the request path to a fixed target. Both apply to every site type.
	StripPrefix bool   `json:"strip_prefix"`
	RewriteTo   string `json:"rewrite_to,omitempty"`
	// Upstream connection tuning (proxy sites only). Streaming disables response
	// buffering so SSE / token-by-token output reaches the browser immediately.
	Streaming             bool   `json:"streaming"`
	DialTimeout           string `json:"dial_timeout,omitempty"`
	ResponseHeaderTimeout string `json:"response_header_timeout,omitempty"`
	// Maintenance short-circuits the final handler with a 503 page while keeping
	// the security middleware chain (headers, rate limit) active.
	Maintenance        bool   `json:"maintenance"`
	MaintenanceMessage string `json:"maintenance_message,omitempty"`
	Version            int64  `json:"version"`
}

type Layer4Rule struct {
	Runtime       *forwardRuntime `json:"runtime,omitempty"`
	ID            int64           `json:"id"`
	Name          string          `json:"name"`
	ListenAddress string          `json:"listen_address"`
	ListenPort    int             `json:"listen_port"`
	Upstream      string          `json:"upstream_address"`
	Protocol      string          `json:"protocol"`
	Enabled       bool            `json:"enabled"`
	Version       int64           `json:"version"`
}

type App struct {
	trafficMu      sync.Mutex
	caddyPID       atomic.Int64
	applyStateMu   sync.RWMutex
	applyState     ApplyState
	configMu       sync.RWMutex
	listenerMu     sync.Mutex
	db             *sql.DB
	adminURL       string
	adminListen    string
	httpListen     string
	panelListen    string
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
	filesRoot      string
	filesReadOnly  bool
	failCounts     map[string][]time.Time
	applyKick      chan struct{}
	ddnsRunning    map[int64]bool
	ddnsFailures   map[int64]int
	alertMu        sync.Mutex
	alertLast      map[string]time.Time
	healthMu       sync.RWMutex
	health         map[int64]upstreamHealth
	banPolicyCache atomic.Value
	// panelAccessCache holds panelAccessRules; read on every panel request so
	// the guard never touches SQLite on the hot path.
	panelAccessCache atomic.Value
	// Local interface addresses, refreshed lazily, let the guard recognise a
	// request that originated on this host (SSH tunnel recovery path).
	localAddrsMu sync.Mutex
	localAddrs   map[netip.Addr]bool
	localAddrsAt time.Time
	// Hostnames currently reverse-proxying to the panel, discovered from the
	// site table so the one-switch mode needs no manual list.
	panelDomainsMu    sync.Mutex
	panelDomainsCache map[string]bool
	panelDomainsAt    time.Time
	autoBanApplyAt    atomic.Int64
}

var domainRE = regexp.MustCompile(`(?i)^(?:\*\.)?(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)*[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
var headerNameRE = regexp.MustCompile(`^[A-Za-z0-9!#$%&'*+.^_` + "`" + `|~-]+$`)

func main() {
	runCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	dataDir := env("VIYA_DATA_DIR", "./data")
	// Static-site roots below the managed tree must be absolute; resolve the
	// data directory once so a relative VIYA_DATA_DIR (e.g. the default "./data")
	// never produces a relative root that prepareSiteStorage would reject.
	if abs, err := filepath.Abs(dataDir); err == nil {
		dataDir = abs
	}
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
	if err := migrateSchema(db); err != nil {
		slog.Error("database migration failed", "error", err)
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
	secureCookies := env("VIYA_COOKIE_SECURE", "true") != "false"
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
	app := &App{db: db, adminURL: adminURL, adminListen: adminListen, httpListen: httpListen, caddyBin: env("VIYA_CADDY_BIN", filepath.Join("bin", "caddy")), secureCookies: secureCookies, trustedProxies: trustedProxies, ctx: runCtx, masterKey: masterKey, dataDir: dataDir, filesRoot: env("VIYA_FILES_ROOT", "/"), filesReadOnly: env("VIYA_FILES_READONLY", "1") != "0", failCounts: make(map[string][]time.Time), certProgress: make(map[string]certificateProgress), applyKick: make(chan struct{}, 1), ddnsRunning: make(map[int64]bool)}
	app.configureProgramLogging()
	app.loadApplicationJournal()
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
	app.registerRoutes(mux)
	go app.ddnsWorker()
	go app.healthWorker()
	go app.watchAccessLogs()
	go app.watchBanExpiry()
	go app.watchTraffic()
	go app.applyWorker()
	addr := env("VIYA_LISTEN", "127.0.0.1:8181")
	if _, err := app.restoreExpiredPanelPort(runCtx); err != nil {
		slog.Error("restore expired panel port", "error", err)
		os.Exit(1)
	}
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
	app.panelListen = addr
	go app.watchPanelConfirmation()
	go app.watchAccessRules()
	go app.watchRecoveryBackups()
	slog.Info("Viya API listening", "address", addr)
	// WriteTimeout must stay above the slowest business operation (DDNS run 35s,
	// configuration import 90s); otherwise Go truncates a response after the
	// work already succeeded and operators retry the same import twice.
	server := &http.Server{Handler: securityHeaders(logRequests(recoverPanic(app.panelAccessGuard(mux)))), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 120 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 1 << 20}
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
		if err := <-serveErr; err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Warn("HTTP server serve", "error", err)
		}
	}
}

type Revision struct {
	Summary   string `json:"summary"`
	Actor     string `json:"actor,omitempty"`
	ID        int64  `json:"id"`
	Status    string `json:"status"`
	Error     string `json:"error,omitempty"`
	CreatedAt string `json:"created_at"`
}
