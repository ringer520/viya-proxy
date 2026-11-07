package main

import (
	"database/sql"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func sharesTestApp(t *testing.T) (*App, *sql.DB) {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	if _, err = db.Exec(`CREATE TABLE panel_settings(key TEXT PRIMARY KEY, value TEXT NOT NULL);
		CREATE TABLE proxy_hosts(
			id INTEGER PRIMARY KEY, name TEXT NOT NULL DEFAULT '', domain TEXT NOT NULL,
			type TEXT NOT NULL DEFAULT 'proxy', root_path TEXT NOT NULL DEFAULT '',
			path TEXT NOT NULL DEFAULT '', enabled INTEGER NOT NULL DEFAULT 1,
			browse INTEGER NOT NULL DEFAULT 0, webdav INTEGER NOT NULL DEFAULT 0,
			webdav_write INTEGER NOT NULL DEFAULT 0,
			basic_auth_user TEXT NOT NULL DEFAULT '', basic_auth_hash TEXT NOT NULL DEFAULT '',
			listener_id TEXT NOT NULL DEFAULT '', tls_mode TEXT NOT NULL DEFAULT 'auto');`); err != nil {
		t.Fatal(err)
	}
	return &App{db: db}, db
}

func TestListFileSharesFiltersTypesAndNeverLeaksCredentials(t *testing.T) {
	app, db := sharesTestApp(t)
	defer db.Close()
	_, err := db.Exec(`INSERT INTO proxy_hosts(id,name,domain,type,root_path,browse,webdav,webdav_write,basic_auth_user,basic_auth_hash,listener_id,tls_mode) VALUES
		(1,'Share','share.example.com','files','/srv/share',1,1,1,'alice','SECRET-HASH','https-main','auto'),
		(2,'Site','site.example.com','static','/var/lib/viya/sites/site',0,0,0,'','','http-main','none'),
		(3,'Proxy','api.example.com','proxy','',0,0,0,'','','https-main','auto'),
		(4,'Redirect','go.example.com','redirect','',0,0,0,'','','https-main','auto')`)
	if err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	app.listFileShares(rec, httptest.NewRequest("GET", "/api/v1/file-shares", nil))
	if rec.Code != 200 {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	raw := rec.Body.String()
	for _, secret := range []string{"SECRET-HASH", "password_hash", "basic_auth_hash"} {
		if strings.Contains(raw, secret) {
			t.Fatalf("response leaked %q: %s", secret, raw)
		}
	}

	var body struct {
		Shares []fileShareView `json:"shares"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Shares) != 2 {
		t.Fatalf("expected only static/files shares, got %d: %s", len(body.Shares), raw)
	}
	byDomain := map[string]fileShareView{}
	for _, s := range body.Shares {
		byDomain[s.Domain] = s
	}
	share, ok := byDomain["share.example.com"]
	if !ok {
		t.Fatalf("files share missing: %s", raw)
	}
	if !share.AuthEnabled || share.AuthUser != "alice" || !share.WebDAV || !share.WebDAVWrite || !share.Browse {
		t.Fatalf("share flags wrong: %+v", share)
	}
	if share.URL != "https://share.example.com" {
		t.Fatalf("default https URL = %q", share.URL)
	}
	site := byDomain["site.example.com"]
	if site.AuthEnabled || site.WebDAV {
		t.Fatalf("static site flags wrong: %+v", site)
	}
	if site.URL != "http://site.example.com" {
		t.Fatalf("default http URL = %q", site.URL)
	}
}

func TestShareURLKeepsNonDefaultPortAndPath(t *testing.T) {
	listeners := map[string]WebListener{
		"https-alt": {ID: "https-alt", Protocol: "https", Port: 8443},
		"http-alt":  {ID: "http-alt", Protocol: "http", Port: 8080},
	}
	cases := []struct {
		listener, path, want string
	}{
		{"https-alt", "", "https://nas.example.com:8443"},
		{"https-alt", "/docs", "https://nas.example.com:8443/docs"},
		{"https-alt", "docs/", "https://nas.example.com:8443/docs"},
		{"http-alt", "/", "http://nas.example.com:8080"},
		{"missing", "", ""},
	}
	for _, tc := range cases {
		got := shareURL(listeners, "nas.example.com", tc.path, tc.listener)
		if got != tc.want {
			t.Errorf("shareURL(%q, %q) = %q, want %q", tc.listener, tc.path, got, tc.want)
		}
	}
}
