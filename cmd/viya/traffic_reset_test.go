package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestResetTrafficPreservesConfigurationAndDoesNotRecountOldLogs(t *testing.T) {
	db, e := sql.Open("sqlite", ":memory:")
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	_, e = db.Exec(`CREATE TABLE proxy_hosts(id INTEGER PRIMARY KEY,domain TEXT,listener_id TEXT DEFAULT '');INSERT INTO proxy_hosts VALUES(1,'app.test','');CREATE TABLE layer4_rules(id INTEGER PRIMARY KEY,name TEXT,listen_port INTEGER,protocol TEXT);CREATE TABLE panel_settings(key TEXT PRIMARY KEY,value TEXT);INSERT INTO panel_settings VALUES('keep','configuration')`)
	if e != nil {
		t.Fatal(e)
	}
	a := &App{db: db, ctx: context.Background(), dataDir: t.TempDir()}
	if e = a.initTraffic(); e != nil {
		t.Fatal(e)
	}
	event := func(ts int64) []byte {
		b, _ := json.Marshal(map[string]any{"ts": ts, "status": 502, "request": map[string]string{"host": "app.test"}})
		return append(b, '\n')
	}
	old := event(time.Now().Unix() - 60)
	path := filepath.Join(a.dataDir, "access.log")
	os.WriteFile(path, old, 0600)
	if e = a.ingestTraffic(); e != nil {
		t.Fatal(e)
	}
	bad := httptest.NewRecorder()
	a.resetTraffic(bad, httptest.NewRequest("POST", "/", strings.NewReader(`{}`)))
	if bad.Code != 400 {
		t.Fatal("reset did not require confirmation")
	}
	w := httptest.NewRecorder()
	a.resetTraffic(w, httptest.NewRequest("POST", "/", strings.NewReader(`{"confirmed":true}`)))
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	var value string
	db.QueryRow(`SELECT value FROM panel_settings WHERE key='keep'`).Scan(&value)
	if value != "configuration" {
		t.Fatal("configuration changed")
	}
	for _, table := range []string{"traffic_hours", "layer4_traffic_hours", "site_access_events"} {
		var n int
		db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n)
		if n != 0 {
			t.Fatal("not cleared", table)
		}
	}
	os.WriteFile(filepath.Join(a.dataDir, "access-rotated.log"), event(time.Now().Unix()-120), 0600)
	// Restarting and discovering previously unseen archives must not restore old counts.
	a = &App{db: db, ctx: context.Background(), dataDir: a.dataDir}
	if e = a.ingestTraffic(); e != nil {
		t.Fatal(e)
	}
	var n int
	db.QueryRow(`SELECT COUNT(*) FROM traffic_hours`).Scan(&n)
	if n != 0 {
		t.Fatal("old archive was recounted")
	}
	os.WriteFile(path, append(old, event(time.Now().Unix()+1)...), 0600)
	if e = a.ingestTraffic(); e != nil {
		t.Fatal(e)
	}
	db.QueryRow(`SELECT SUM(requests) FROM traffic_hours`).Scan(&n)
	if n != 1 {
		t.Fatal("new traffic did not resume", n)
	}
}

func TestConfigRestorePreservesTrafficResetBoundary(t *testing.T) {
	db, e := sql.Open("sqlite", ":memory:")
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	_, e = db.Exec(`CREATE TABLE panel_settings(key TEXT PRIMARY KEY,value TEXT);INSERT INTO panel_settings VALUES('traffic_reset_at','123.500')`)
	if e != nil {
		t.Fatal(e)
	}
	tx, e := db.Begin()
	if e != nil {
		t.Fatal(e)
	}
	tables := []tableDump{{Name: "panel_settings", Columns: []string{"key", "value"}, Rows: [][]configCell{{{Type: "text", Value: "keep"}, {Type: "text", Value: "restored"}}}}}
	if e = restoreTables(context.Background(), tx, tables); e != nil {
		tx.Rollback()
		t.Fatal(e)
	}
	if e = tx.Commit(); e != nil {
		t.Fatal(e)
	}
	var value string
	db.QueryRow(`SELECT value FROM panel_settings WHERE key='traffic_reset_at'`).Scan(&value)
	if value != "123.500" {
		t.Fatal("config restore lost the traffic reset boundary")
	}
}
