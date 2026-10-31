package main

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestListenerMigrationPreservesSitesAndForeignKeys(t *testing.T) {
	db, e := sql.Open("sqlite", ":memory:")
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	_, e = db.Exec(`PRAGMA foreign_keys=ON;CREATE TABLE panel_settings(key TEXT PRIMARY KEY,value TEXT NOT NULL);CREATE TABLE proxy_hosts(id INTEGER PRIMARY KEY,domain TEXT NOT NULL UNIQUE,tls_mode TEXT NOT NULL DEFAULT 'auto',upstream TEXT NOT NULL DEFAULT '');INSERT INTO proxy_hosts(id,domain,tls_mode,upstream) VALUES(1,'app.example.com','auto','https://backend.example.com'),(2,'plain.example.com','none','http://backend.example.com');CREATE TABLE events(id INTEGER PRIMARY KEY,site_id INTEGER REFERENCES proxy_hosts(id) ON DELETE CASCADE);INSERT INTO events VALUES(1,1);`)
	if e != nil {
		t.Fatal(e)
	}
	a := &App{db: db}
	if e = a.initListenerSites(); e != nil {
		t.Fatal(e)
	}
	var listener string
	if e = db.QueryRow(`SELECT listener_id FROM proxy_hosts WHERE id=1`).Scan(&listener); e != nil || listener != "https-main" {
		t.Fatalf("https migration: %q %v", listener, e)
	}
	tx, e := db.Begin()
	if e != nil {
		t.Fatal(e)
	}
	if e = cloneListenerSites(context.Background(), tx, "https-main", "https-copy", "https"); e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(); e != nil {
		t.Fatal(e)
	}
	_, e = db.Exec(`UPDATE proxy_hosts SET upstream='https://changed.example.com' WHERE listener_id='https-copy'`)
	if e != nil {
		t.Fatal(e)
	}
	var upstream string
	_ = db.QueryRow(`SELECT upstream FROM proxy_hosts WHERE id=1`).Scan(&upstream)
	if upstream != "https://backend.example.com" {
		t.Fatal("cloned listener modified the original site")
	}
	var count int
	_ = db.QueryRow(`SELECT COUNT(*) FROM events`).Scan(&count)
	if count != 1 {
		t.Fatal("migration lost existing traffic")
	}
	if e = a.initListenerSites(); e != nil {
		t.Fatal(e)
	}
	_ = db.QueryRow(`SELECT COUNT(*) FROM proxy_hosts`).Scan(&count)
	if count != 3 {
		t.Fatalf("migration is not idempotent: %d", count)
	}
	_, e = db.Exec(`DELETE FROM proxy_hosts WHERE id=1`)
	if e != nil {
		t.Fatal(e)
	}
	_ = db.QueryRow(`SELECT COUNT(*) FROM events`).Scan(&count)
	if count != 0 {
		t.Fatal("foreign keys were not restored")
	}
}
func archiveFixture() configPayload {
	p := configPayload{Format: configFormat, Key: make([]byte, 32), SourceDir: "/var/lib/viya", Preferences: map[string]string{"viya-theme": "dark"}}
	for _, name := range configTables {
		table := tableDump{Name: name, Columns: []string{"id"}, Rows: [][]configCell{}}
		if name == "users" {
			table.Columns = []string{"id", "username", "password_hash"}
			table.Rows = [][]configCell{{{Type: "integer", Value: "1"}, {Type: "text", Value: "admin"}, {Type: "blob", Value: base64.StdEncoding.EncodeToString([]byte("$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy"))}}}
		}
		if name == "dns_providers" {
			table.Columns = []string{"id", "credentials_nonce", "credentials_ciphertext"}
		}
		p.Tables = append(p.Tables, table)
	}
	return p
}

const testArchivePassword = "viya-test-password"

func TestEncryptedConfigurationRoundTrip(t *testing.T) {
	p := archiveFixture()
	archive, e := sealConfiguration(p, testArchivePassword)
	if e != nil {
		t.Fatal(e)
	}
	if !archive.Encrypted || archive.Payload != nil {
		t.Fatal("archive must be encrypted and must not carry a plaintext payload")
	}
	raw, _ := json.Marshal(archive)
	for _, leak := range []string{"viya-theme", "password_hash", `"users"`, "custom-certs", "/var/lib/viya"} {
		if strings.Contains(string(raw), leak) {
			t.Fatalf("encrypted archive leaks %q", leak)
		}
	}
	if _, e = openConfiguration(configImportInput{Archive: archive}); e == nil {
		t.Fatal("accepted an encrypted archive without a password")
	}
	if _, e = openConfiguration(configImportInput{Archive: archive, Password: "wrong-password"}); e == nil {
		t.Fatal("accepted an incorrect backup password")
	}
	opened, e := openConfiguration(configImportInput{Archive: archive, Password: testArchivePassword})
	if e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(p.Tables, opened.Tables) {
		t.Fatal("configuration round trip changed tables")
	}
	if reflect.DeepEqual(p.Key, opened.Key) {
		t.Fatal("installation key was exported")
	}
	for _, table := range opened.Tables {
		if table.Name == "users" || table.Name == "sessions" {
			t.Fatal("account information exported")
		}
	}
}
func TestConfigurationRejectsEscapingFilesAndMalformedRows(t *testing.T) {
	for _, path := range []string{"../master.key", "custom-certs/../../outside.key", "/etc/passwd", "custom-certs/../../.aws/config", "C:/Windows/test"} {
		p := archiveFixture()
		p.Files = []configFile{{Path: path, Data: []byte("not a real key")}}
		archive, e := sealConfiguration(p, testArchivePassword)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = openConfiguration(configImportInput{Archive: archive, Password: testArchivePassword}); e == nil {
			t.Fatalf("accepted path: %s", path)
		}
	}
	p := archiveFixture()
	p.Tables[0].Rows = [][]configCell{nil}
	archive, _ := sealConfiguration(p, testArchivePassword)
	if _, e := openConfiguration(configImportInput{Archive: archive, Password: testArchivePassword}); e == nil {
		t.Fatal("accepted malformed row")
	}
}
func TestArchiveRequiresStrongPassword(t *testing.T) {
	if _, err := sealConfiguration(archiveFixture(), "short"); err == nil {
		t.Fatal("accepted a backup password shorter than the minimum")
	}
}
func TestLegacyPasswordFreeArchiveStillImports(t *testing.T) {
	p := archiveFixture()
	archive, err := sealRecoverySnapshot(p)
	if err != nil {
		t.Fatal(err)
	}
	opened, err := openConfiguration(configImportInput{Archive: archive})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(p.Tables, opened.Tables) {
		t.Fatal("legacy archive round trip changed tables")
	}
}
func TestCertificateWildcardDepth(t *testing.T) {
	for name, want := range map[string]bool{"viya.example.com": true, "qb.viya.example.com": false, "example.com": false} {
		if got := certificateMatchesDomain([]string{"*.example.com"}, name); got != want {
			t.Fatalf("%s: %v != %v", name, got, want)
		}
	}
}
func TestRestoreRejectsUnknownColumnsWithoutChangingData(t *testing.T) {
	db, _ := sql.Open("sqlite", ":memory:")
	defer db.Close()
	db.SetMaxOpenConns(1)
	_, e := db.Exec(`CREATE TABLE panel_settings(key TEXT PRIMARY KEY,value TEXT);INSERT INTO panel_settings VALUES('known','preserve')`)
	if e != nil {
		t.Fatal(e)
	}
	tx, _ := db.Begin()
	e = restoreTables(context.Background(), tx, []tableDump{{Name: "panel_settings", Columns: []string{"key", "injected"}, Rows: [][]configCell{}}})
	if e == nil {
		t.Fatal("accepted unknown column")
	}
	tx.Rollback()
	var value string
	_ = db.QueryRow(`SELECT value FROM panel_settings WHERE key='known'`).Scan(&value)
	if value != "preserve" {
		t.Fatal("invalid import altered current settings")
	}
}
func TestArchiveIsEncryptedAndPortable(t *testing.T) {
	p := archiveFixture()
	p.Preferences["viya-theme"] = "dark"
	archive, err := sealConfiguration(p, testArchivePassword)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(archive)
	if strings.Contains(string(raw), "dark") || strings.Contains(string(raw), "password_hash") || strings.Contains(string(raw), `"users"`) {
		t.Fatal("incorrect archive scope")
	}
	// The portable key is generated per export and differs from the
	// installation master key inside the ciphertext.
	opened, err := openConfiguration(configImportInput{Archive: archive, Password: testArchivePassword})
	if err != nil {
		t.Fatal(err)
	}
	if len(opened.Key) != 32 || reflect.DeepEqual(opened.Key, p.Key) {
		t.Fatal("archive key is not a fresh portable key")
	}
}
