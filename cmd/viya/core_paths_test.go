package main

import (
	"database/sql"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

// These cover the paths that are easiest to break silently: listener conflict
// rules, upstream normalisation, and the input validation that stands between
// the form and the Caddy configuration.
func newTestApp(t *testing.T) (*App, *sql.DB) {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	if _, err = db.Exec(`CREATE TABLE layer4_rules(id INTEGER PRIMARY KEY, name TEXT NOT NULL, listen_address TEXT NOT NULL DEFAULT "0.0.0.0", listen_port INTEGER NOT NULL, upstream TEXT NOT NULL, protocol TEXT NOT NULL, enabled INTEGER NOT NULL DEFAULT 1, version INTEGER NOT NULL DEFAULT 1, created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP, updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP);
		CREATE TABLE IF NOT EXISTS panel_settings(key TEXT PRIMARY KEY, value TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	return &App{db: db, adminListen: "127.0.0.1:2019", httpListen: ":8080"}, db
}

func TestLayer4ConflictDetectsSamePortAndProtocol(t *testing.T) {
	app, db := newTestApp(t)
	defer db.Close()
	if _, err := db.Exec(`INSERT INTO layer4_rules(name,listen_address,listen_port,upstream,protocol) VALUES('a','0.0.0.0',1082,'192.168.1.10:80','tcp')`); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("POST", "/api/v1/layer4-rules", nil)
	conflict := Layer4Rule{Name: "b", ListenAddress: "0.0.0.0", ListenPort: 1082, Upstream: "192.168.1.11:80", Protocol: "tcp"}
	if err := app.checkLayer4Conflict(request, 0, conflict); err == nil {
		t.Fatal("expected a conflict on the same port and protocol")
	}
	// UDP may share the port with an existing TCP rule.
	allowed := Layer4Rule{Name: "c", ListenAddress: "0.0.0.0", ListenPort: 1082, Upstream: "192.168.1.11:53", Protocol: "udp"}
	if err := app.checkLayer4Conflict(request, 0, allowed); err != nil {
		t.Fatalf("udp beside tcp should be allowed: %v", err)
	}
}

func TestLayer4ConflictRejectsReservedPorts(t *testing.T) {
	app, db := newTestApp(t)
	defer db.Close()
	request := httptest.NewRequest("POST", "/api/v1/layer4-rules", nil)
	for _, port := range []int{2019, 8080} {
		rule := Layer4Rule{Name: "x", ListenAddress: "0.0.0.0", ListenPort: port, Upstream: "192.168.1.10:80", Protocol: "tcp"}
		if err := app.checkLayer4Conflict(request, 0, rule); err == nil {
			t.Fatalf("port %d is reserved and must be rejected", port)
		}
	}
}

func TestLayer4ConflictIgnoresDisabledRules(t *testing.T) {
	app, db := newTestApp(t)
	defer db.Close()
	if _, err := db.Exec(`INSERT INTO layer4_rules(name,listen_address,listen_port,upstream,protocol,enabled) VALUES('a','0.0.0.0',1082,'192.168.1.10:80','tcp',0)`); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("POST", "/api/v1/layer4-rules", nil)
	rule := Layer4Rule{Name: "b", ListenAddress: "0.0.0.0", ListenPort: 1082, Upstream: "192.168.1.11:80", Protocol: "tcp"}
	if err := app.checkLayer4Conflict(request, 0, rule); err != nil {
		t.Fatalf("a disabled rule should not block the port: %v", err)
	}
}

func TestValidateLayer4RuleRejectsBadInput(t *testing.T) {
	bad := []Layer4Rule{
		{Name: "host not an ip", ListenAddress: "example.com", ListenPort: 1082, Upstream: "192.168.1.10:80", Protocol: "tcp"},
		{Name: "port out of range", ListenAddress: "0.0.0.0", ListenPort: 70000, Upstream: "192.168.1.10:80", Protocol: "tcp"},
		{Name: "bad protocol", ListenAddress: "0.0.0.0", ListenPort: 1082, Upstream: "192.168.1.10:80", Protocol: "sctp"},
		{Name: "upstream without port", ListenAddress: "0.0.0.0", ListenPort: 1082, Upstream: "192.168.1.10", Protocol: "tcp"},
		{Name: "newline inside port", ListenAddress: "0.0.0.0", ListenPort: 1082, Upstream: "192.168.1.10:8\n0", Protocol: "tcp"},
	}
	for _, rule := range bad {
		item := rule
		if err := validateLayer4Rule(&item); err == nil {
			t.Errorf("validateLayer4Rule accepted %s", rule.Name)
		}
	}
	item := Layer4Rule{ListenPort: 1082, Upstream: "[fd00::20]:80", Protocol: "both"}
	if err := validateLayer4Rule(&item); err != nil {
		t.Fatalf("valid rule rejected: %v", err)
	}
	if item.ListenAddress != "0.0.0.0" {
		t.Fatalf("empty listen address should default to 0.0.0.0, got %q", item.ListenAddress)
	}
	// Surrounding whitespace is trimmed rather than rejected; what matters is
	// that nothing dangerous survives into the stored value.
	trimmed := Layer4Rule{ListenPort: 1082, Upstream: " 192.168.1.10:80\n", Protocol: "tcp"}
	if err := validateLayer4Rule(&trimmed); err != nil {
		t.Fatalf("surrounding whitespace should be trimmed: %v", err)
	}
	if strings.ContainsAny(trimmed.Upstream, "\r\n") {
		t.Fatalf("newline survived validation: %q", trimmed.Upstream)
	}
}

func TestUpstreamDialTargetNormalisesForms(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"http://192.168.1.10:28883", "192.168.1.10:28883"},
		{"https://192.168.1.10", "192.168.1.10:443"},
		{"http://app.local", "app.local:80"},
		{"192.168.1.10:9000", "192.168.1.10:9000"},
		{"192.168.1.10", "192.168.1.10:80"},
		{"http://[fd00::20]:8080", "[fd00::20]:8080"},
		{"http://192.168.1.10:8080/api", "192.168.1.10:8080"},
		{"", ""},
	}
	for _, test := range tests {
		if got := upstreamDialTarget(test.input); got != test.want {
			t.Errorf("upstreamDialTarget(%q) = %q, want %q", test.input, got, test.want)
		}
	}
}

func TestValidWebhookURLAcceptsOnlyHTTP(t *testing.T) {
	for _, good := range []string{"https://hooks.example.com/abc", "http://192.168.1.9:8080/hook"} {
		if !validWebhookURL(good) {
			t.Errorf("validWebhookURL rejected %q", good)
		}
	}
	for _, bad := range []string{"", "ftp://example.com", "javascript:alert(1)", "not a url", "file:///etc/passwd"} {
		if validWebhookURL(bad) {
			t.Errorf("validWebhookURL accepted %q", bad)
		}
	}
}

func TestValidateUsernameRejectsShellUnsafeNames(t *testing.T) {
	for _, bad := range []string{"ab", "", strings.Repeat("a", 65), "user name", "user@host", "rm -rf"} {
		if validateUsername(bad) != "" {
			t.Errorf("validateUsername accepted %q", bad)
		}
	}
	for _, good := range []string{"admin", "ryan_2", "read.only", "ops-1"} {
		if validateUsername(good) == "" {
			t.Errorf("validateUsername rejected %q", good)
		}
	}
}

func TestCertificateMatchesDomainWildcardDepth(t *testing.T) {
	if !certificateMatchesDomain([]string{"*.example.com"}, "app.example.com") {
		t.Error("wildcard should cover one label")
	}
	if certificateMatchesDomain([]string{"*.example.com"}, "deep.app.example.com") {
		t.Error("wildcard must not cover two labels")
	}
	if !certificateMatchesDomain([]string{"APP.example.com"}, "app.example.com") {
		t.Error("comparison must be case-insensitive")
	}
	if certificateMatchesDomain([]string{"other.example.com"}, "app.example.com") {
		t.Error("unrelated name must not match")
	}
}

func TestFlattenJSONProducesStablePaths(t *testing.T) {
	var value any
	tree := `{"apps":{"http":{"servers":{"s":{"routes":[{"match":[{"host":["a.test"]}]}]}}}}}`
	if err := json.Unmarshal([]byte(tree), &value); err != nil {
		t.Fatal(err)
	}
	flat := map[string]string{}
	if !flattenJSON("", value, flat) {
		t.Fatal("flatten reported truncation on a tiny document")
	}
	want := "apps.http.servers.s.routes[0].match[0].host[0]"
	if flat[want] != "a.test" {
		t.Fatalf("expected %q to equal a.test, got %q (paths: %v)", want, flat[want], flat)
	}
	if len(flat) != 1 {
		t.Fatalf("unexpected leaf count %d: %v", len(flat), flat)
	}
}
