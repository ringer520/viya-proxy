package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
	"viya/internal/secretstore"
)

func TestEncryptedCheckpointRejectsTampering(t *testing.T) {
	a := &App{dataDir: t.TempDir(), masterKey: bytes.Repeat([]byte{7}, 32)}
	payload := checkpoint{Hash: "hash", Created: "today"}
	plain, _ := json.Marshal(payload)
	nonce, data, e := secretstore.Encrypt(a.masterKey, plain, []byte("viya:checkpoint:v1"))
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(sealedCheckpoint{nonce, data})
	if e = os.MkdirAll(filepath.Dir(a.checkpointPath("current")), 0700); e != nil {
		t.Fatal(e)
	}
	if e = atomicRecoveryFile(a.checkpointPath("current"), raw); e != nil {
		t.Fatal(e)
	}
	if cp, e := a.readCheckpoint("current"); e != nil || cp.Hash != "hash" {
		t.Fatalf("read: %v %+v", e, cp)
	}
	data[0] ^= 1
	raw, _ = json.Marshal(sealedCheckpoint{nonce, data})
	if e = atomicRecoveryFile(a.checkpointPath("current"), raw); e != nil {
		t.Fatal(e)
	}
	if _, e = a.readCheckpoint("current"); e == nil {
		t.Fatal("tampered checkpoint accepted")
	}
}
func TestPanelPortExpiryAndConfirmation(t *testing.T) {
	a, db := newTestApp(t)
	defer db.Close()
	ctx := context.Background()
	raw, _ := json.Marshal(panelConfirmation{OldPort: "8181", NewPort: "8282", Deadline: time.Now().Add(-time.Second).Unix()})
	if e := writePanelSetting(ctx, a, "pending_panel_port", string(raw)); e != nil {
		t.Fatal(e)
	}
	ok, e := a.restoreExpiredPanelPort(ctx)
	if e != nil || !ok {
		t.Fatalf("restore: %v %v", ok, e)
	}
	var port string
	if e = db.QueryRow(`SELECT value FROM panel_settings WHERE key='listen_port'`).Scan(&port); e != nil || port != "8181" {
		t.Fatalf("port=%s err=%v", port, e)
	}
	if a.pendingPanelConfirmation(ctx) != nil {
		t.Fatal("guard not cleared")
	}
	raw, _ = json.Marshal(panelConfirmation{OldPort: "8181", NewPort: "8282", Deadline: time.Now().Add(time.Minute).Unix()})
	writePanelSetting(ctx, a, "pending_panel_port", string(raw))
	a.panelListen = "127.0.0.1:8181"
	w := httptest.NewRecorder()
	a.confirmPanelAccess(w, httptest.NewRequest("POST", "/", nil))
	if w.Code != 409 {
		t.Fatalf("old endpoint confirmed: %d", w.Code)
	}
	a.panelListen = "127.0.0.1:8282"
	w = httptest.NewRecorder()
	a.confirmPanelAccess(w, httptest.NewRequest("POST", "/", nil))
	if w.Code != 204 || a.pendingPanelConfirmation(ctx) != nil {
		t.Fatalf("confirmation failed: %d", w.Code)
	}
}
func TestSiteMetadataRequiresExistingSiteAndLimitsTags(t *testing.T) {
	a, db := newTestApp(t)
	defer db.Close()
	db.Exec(`CREATE TABLE proxy_hosts(id INTEGER PRIMARY KEY,domain TEXT);INSERT INTO proxy_hosts VALUES(1,'app.localhost')`)
	for _, body := range []string{`{"tags":["a","a","b"],"favorite":true}`, `{"tags":["` + strings.Repeat("a", 25) + `"]}`} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("PATCH", "/", strings.NewReader(body))
		r.SetPathValue("id", "1")
		a.updateSiteMetadata(w, r)
		if strings.Contains(body, "favorite") {
			if w.Code != 200 {
				t.Fatal(w.Body.String())
			}
			hosts := []Host{{ID: 1, Domain: "app.localhost"}}
			a.attachSiteMetadata(context.Background(), hosts)
			if !hosts[0].Favorite || len(hosts[0].Tags) != 2 {
				t.Fatalf("metadata: %+v", hosts[0])
			}
		} else if w.Code != 400 {
			t.Fatalf("accepted excessive tag: %d", w.Code)
		}
	}
}

func TestAutomaticBackupRetention(t *testing.T) {
	a := &App{dataDir: t.TempDir()}
	dir := filepath.Dir(a.checkpointPath("current"))
	os.MkdirAll(dir, 0700)
	os.WriteFile(a.checkpointPath("current"), []byte("encrypted"), 0600)
	os.WriteFile(a.checkpointPath("previous"), []byte("previous"), 0600)
	for i := 1; i <= 9; i++ {
		name := "auto-2020-01-0" + strconv.Itoa(i)
		os.WriteFile(a.checkpointPath(name), []byte("old"), 0600)
	}
	if e := a.archiveDailyCheckpoint(); e != nil {
		t.Fatal(e)
	}
	files, _ := os.ReadDir(dir)
	count := 0
	for _, f := range files {
		if automaticBackupName.MatchString(strings.TrimSuffix(f.Name(), ".json")) {
			count++
		}
	}
	if count != 7 {
		t.Fatalf("retained %d backups", count)
	}
	if _, e := os.Stat(a.checkpointPath("previous")); e != nil {
		t.Fatal("previous recovery point removed")
	}
}
func TestDiagnosticsInvalidEscapedPathDoesNotPanic(t *testing.T) {
	a, db := newTestApp(t)
	defer db.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	defer server.Close()
	u, _ := url.Parse(server.URL)
	port, _ := strconv.Atoi(u.Port())
	raw, _ := json.Marshal([]WebListener{{ID: "local", Protocol: "http", Port: port}, {ID: "secure", Protocol: "https", Port: 18444}})
	writePanelSetting(context.Background(), a, "web_listeners", string(raw))
	_, e := db.Exec(`CREATE TABLE proxy_hosts(id INTEGER PRIMARY KEY,domain TEXT,listener_id TEXT,upstream TEXT,type TEXT,root_path TEXT,path TEXT,tls_mode TEXT,enabled INTEGER,custom_certificate_id INTEGER);INSERT INTO proxy_hosts VALUES(1,'localhost','local','','static',?,'/%zz','none',1,NULL)`, t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	r := httptest.NewRequest("GET", "/", nil)
	r.SetPathValue("id", "1")
	w := httptest.NewRecorder()
	a.diagnoseSite(w, r)
	if w.Code != 200 {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "访问路径无法生成有效请求") {
		t.Fatalf("missing path failure: %s", w.Body.String())
	}
}
