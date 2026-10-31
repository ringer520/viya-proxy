package main

import (
	"context"
	"database/sql"
	"testing"
	"time"
)

func TestAutomaticBansPreserveManualRanges(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	expires := time.Now().Add(time.Minute).Unix()
	if _, err = db.Exec(`CREATE TABLE ip_bans(id INTEGER PRIMARY KEY,domain TEXT,ip TEXT,reason TEXT,expires_at INTEGER,source TEXT DEFAULT 'auto',UNIQUE(domain,ip));INSERT INTO ip_bans(domain,ip,reason,expires_at,source) VALUES('app.test','192.0.2.0/24','manual',?,'manual')`, expires); err != nil {
		t.Fatal(err)
	}
	a := &App{db: db, ctx: context.Background(), failCounts: map[string][]time.Time{}}
	for i := 0; i < 21; i++ {
		if a.observeAttack([]byte(`{"status":403,"request":{"host":"app.test","remote_ip":"192.0.2.10"}}`)) {
			t.Fatal("automatic ban replaced manual range")
		}
	}
	var n int
	var actual int64
	if err = db.QueryRow(`SELECT COUNT(*),MAX(expires_at) FROM ip_bans`).Scan(&n, &actual); err != nil {
		t.Fatal(err)
	}
	if n != 1 || actual != expires {
		t.Fatalf("manual rule changed: %d %d", n, actual)
	}
}

func TestBanValidation(t *testing.T) {
	// Bans are permanent by default (ExpiresAt == 0); an explicit expiry only
	// has to be in the future.
	for _, test := range []struct {
		ip     string
		expiry int64
		valid  bool
	}{{"192.0.2.1", 0, true}, {"2001:db8::/64", 0, true}, {"invalid", 0, false}, {"192.0.2.1", time.Now().Add(-time.Hour).Unix(), false}, {"192.0.2.1", time.Now().AddDate(2, 0, 0).Unix(), true}} {
		v := IPBan{Domain: "app.example.com", IP: test.ip, ExpiresAt: test.expiry}
		err := validateBan(&v)
		if (err == nil) != test.valid {
			t.Fatalf("%+v: %v", test, err)
		}
	}
}
func TestManagedCertificateValidation(t *testing.T) {
	for _, test := range []struct {
		domain, method string
		provider       *int64
		valid          bool
	}{{"*.example.com", "http-01", nil, false}, {"*.example.com", "dns-01", nil, false}, {"app.example.com:443", "http-01", nil, false}, {"https://app.example.com", "http-01", nil, false}, {"test.localhost", "http-01", nil, false}, {"test.localhost", "internal", nil, true}, {"app.example.com", "http-01", nil, true}} {
		v := ManagedCertificate{Name: "test", Domain: test.domain, Method: test.method, ProviderID: test.provider}
		if err := validateManagedCertificate(&v); (err == nil) != test.valid {
			t.Fatalf("%+v: %v", test, err)
		}
	}
}
func TestManagedIssuerPolicies(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	a := &App{db: db}
	if err = a.initCertificateRequests(); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO managed_certificates(name,domain,method,email) VALUES('internal','test.localhost','internal',''),('public','app.example.com','http-01','admin@example.com')`); err != nil {
		t.Fatal(err)
	}
	policies, domains, internal, err := a.managedPolicies(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !internal || len(domains) != 2 || len(policies) != 2 {
		t.Fatalf("unexpected policies: %v %v %v", policies, domains, internal)
	}
	acme := policies[0].(map[string]any)["issuers"].([]any)[0].(map[string]any)
	if acme["module"] != "acme" || acme["email"] != "admin@example.com" {
		t.Fatalf("unexpected ACME issuer: %v", acme)
	}
	challenges := acme["challenges"].(map[string]any)
	if challenges["tls-alpn"].(map[string]any)["disabled"] != true {
		t.Fatal("HTTP-01 must disable TLS-ALPN fallback")
	}
}
