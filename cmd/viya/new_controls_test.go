package main

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"
)

func TestMainlandPolicyIncludesIPv6AndPrivateNetworks(t *testing.T) {
	ranges := chinaAndPrivateRanges()
	if len(ranges) < 1000 {
		t.Fatal("missing allocation dataset")
	}
	prefixes := []netip.Prefix{}
	for _, s := range ranges {
		p, e := netip.ParsePrefix(s)
		if e != nil {
			t.Fatal(e)
		}
		prefixes = append(prefixes, p)
	}
	for _, tc := range []struct {
		ip    string
		allow bool
	}{{"114.114.114.114", true}, {"223.5.5.5", true}, {"2400:3200::1", true}, {"127.0.0.1", true}, {"192.168.1.16", true}, {"::1", true}, {"fd00::1", true}, {"8.8.8.8", false}, {"2606:4700:4700::1111", false}, {"1.1.1.1", false}} {
		a := netip.MustParseAddr(tc.ip)
		allowed := false
		for _, p := range prefixes {
			if p.Contains(a) {
				allowed = true
				break
			}
		}
		if allowed != tc.allow {
			t.Errorf("%s: got %v", tc.ip, allowed)
		}
	}
	ban := IPBan{Domain: "app.example.com", IP: "foreign", ExpiresAt: time.Now().Add(time.Hour).Unix()}
	if e := validateBan(&ban); e != nil {
		t.Fatal(e)
	}
	ban.IP = "foreign-unvalidated"
	if validateBan(&ban) == nil {
		t.Fatal("accepted unknown special rule")
	}
}
func TestCertificateEditKeepsDomainIdentity(t *testing.T) {
	db, e := sql.Open("sqlite", ":memory:")
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	a := &App{db: db}
	if e = a.initCertificateRequests(); e != nil {
		t.Fatal(e)
	}
	_, e = db.Exec(`CREATE TABLE proxy_hosts(domain TEXT,tls_mode TEXT); INSERT INTO managed_certificates(id,name,domain,method) VALUES(1,'original','one.localhost','internal')`)
	if e != nil {
		t.Fatal(e)
	}
	r := httptest.NewRequest(http.MethodPut, "/", strings.NewReader(`{"name":"edit","domain":"two.localhost","method":"internal"}`))
	r.SetPathValue("id", "1")
	w := httptest.NewRecorder()
	a.requestCertificate(w, r)
	if w.Code != 400 {
		t.Fatalf("unexpected status %d: %s", w.Code, w.Body.String())
	}
	var domain string
	db.QueryRow(`SELECT domain FROM managed_certificates WHERE id=1`).Scan(&domain)
	if domain != "one.localhost" {
		t.Fatal("domain changed")
	}
}
func TestSiteSwitchRejectsStaleVersion(t *testing.T) {
	db, e := sql.Open("sqlite", ":memory:")
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	_, e = db.Exec(`CREATE TABLE proxy_hosts(id INTEGER PRIMARY KEY,enabled INTEGER,version INTEGER,updated_at TEXT);INSERT INTO proxy_hosts VALUES(1,1,5,'')`)
	if e != nil {
		t.Fatal(e)
	}
	a := &App{db: db}
	r := httptest.NewRequest(http.MethodPatch, "/", strings.NewReader(`{"enabled":false,"version":4}`))
	r.SetPathValue("id", "1")
	w := httptest.NewRecorder()
	a.toggleHostEnabled(w, r)
	if w.Code != 409 {
		t.Fatalf("status %d", w.Code)
	}
	var enabled int
	db.QueryRow(`SELECT enabled FROM proxy_hosts WHERE id=1`).Scan(&enabled)
	if enabled != 1 {
		t.Fatal("stale update changed state")
	}
}

func TestDNSCredentialDeletionChecksAllCertificateReferences(t *testing.T) {
	for _, used := range []string{"proxy_hosts", "managed_certificates", "ddns_records", ""} {
		t.Run(used, func(t *testing.T) {
			db, e := sql.Open("sqlite", ":memory:")
			if e != nil {
				t.Fatal(e)
			}
			defer db.Close()
			_, e = db.Exec(`CREATE TABLE dns_providers(id INTEGER PRIMARY KEY);INSERT INTO dns_providers VALUES(1);CREATE TABLE proxy_hosts(dns_provider_id INTEGER,id INTEGER DEFAULT 1,domain TEXT DEFAULT 'site.test');CREATE TABLE managed_certificates(dns_provider_id INTEGER,id INTEGER DEFAULT 1,domain TEXT DEFAULT 'cert.test');CREATE TABLE ddns_records(dns_provider_id INTEGER,id INTEGER DEFAULT 1,name TEXT DEFAULT 'record')`)
			if e != nil {
				t.Fatal(e)
			}
			if used != "" {
				_, e = db.Exec(`INSERT INTO ` + used + `(dns_provider_id) VALUES(1)`)
				if e != nil {
					t.Fatal(e)
				}
			}
			a := &App{db: db}
			r := httptest.NewRequest(http.MethodDelete, "/", nil)
			r.SetPathValue("id", "1")
			w := httptest.NewRecorder()
			a.deleteDNSProvider(w, r)
			want := 204
			if used != "" {
				want = 409
			}
			if w.Code != want {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
		})
	}
}

func TestLogSettingsAndProgramURLRedaction(t *testing.T) {
	valid := defaultLogSettings()
	if validateLogSettings(&valid) != nil {
		t.Fatal("invalid defaults")
	}
	invalid := valid
	invalid.ProgramMB = 0
	if validateLogSettings(&invalid) == nil {
		t.Fatal("unbounded logging accepted")
	}
	invalid = valid
	invalid.Level = "TRACE"
	if validateLogSettings(&invalid) == nil {
		t.Fatal("invalid level accepted")
	}
	redacted := sanitizeProgramText("connect https://alice:secret@example.com/path?token=private#fragment failed")
	for _, secret := range []string{"alice", "secret", "private", "fragment"} {
		if strings.Contains(redacted, secret) {
			t.Fatal("program log URL leaked credentials")
		}
	}
}
