package main

import (
	"compress/gzip"
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

func TestTrafficPersistsAndFollowsRotation(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if _, err = db.Exec(`PRAGMA foreign_keys=ON; CREATE TABLE proxy_hosts(id INTEGER PRIMARY KEY,domain TEXT,listener_id TEXT NOT NULL DEFAULT '');INSERT INTO proxy_hosts(id,domain) VALUES(1,'app.test'),(2,'*.test'),(3,'other.test')`); err != nil {
		t.Fatal(err)
	}
	a := &App{db: db, ctx: context.Background(), dataDir: t.TempDir()}
	if err = a.initTraffic(); err != nil {
		t.Fatal(err)
	}
	line := []byte(`{"ts":1793318400,"status":200,"bytes_read":12,"size":34,"duration":0.01,"request":{"host":"app.test:8080","method":"POST","uri":"/hello?token=SECRET","remote_ip":"192.0.2.1","headers":{"Authorization":["SECRET"]}}}` + "\n")
	path := filepath.Join(a.dataDir, "access.log")
	if err = os.WriteFile(path, line, 0600); err != nil {
		t.Fatal(err)
	}
	check := func(want int) {
		t.Helper()
		if err = a.ingestTraffic(); err != nil {
			t.Fatal(err)
		}
		var n, up, down int
		if err = db.QueryRow(`SELECT SUM(requests),SUM(upload),SUM(download) FROM traffic_hours WHERE site_id=1`).Scan(&n, &up, &down); err != nil {
			t.Fatal(err)
		}
		if n != want || up != 12*want || down != 34*want {
			t.Fatalf("got requests=%d upload=%d download=%d", n, up, down)
		}
	}
	check(1)
	check(1)
	// Simulated restart retains the same persisted cursor.
	a = &App{db: db, ctx: context.Background(), dataDir: a.dataDir}
	check(1)
	// Rotate to compressed file; do not recount requests already consumed.
	archive, err := os.Create(filepath.Join(a.dataDir, "access-rotated.log.gz"))
	if err != nil {
		t.Fatal(err)
	}
	z := gzip.NewWriter(archive)
	z.Write(line)
	z.Close()
	archive.Close()
	second := []byte(`{"ts":1793318401,"status":500,"bytes_read":12,"size":34,"request":{"host":"app.test","uri":"/second"}}` + "\n")
	if err = os.WriteFile(path, second, 0600); err != nil {
		t.Fatal(err)
	}
	check(2)
	check(2)
	var secretCount int
	if err = db.QueryRow(`SELECT COUNT(*) FROM site_access_events WHERE event LIKE '%SECRET%'`).Scan(&secretCount); err != nil {
		t.Fatal(err)
	}
	if secretCount != 0 {
		t.Fatal("secret leaked into stored site logs")
	}
	// A partial final line is consumed only after its newline arrives.
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	f.Write(line[:len(line)-1])
	f.Close()
	check(2)
	f, _ = os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	f.WriteString("\n")
	f.Close()
	check(3)
}
func TestReadLogLinesLeavesPartialLineForNextRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "access.log")
	if err := os.WriteFile(path, []byte("{\"a\":1}\n{\"b\":2"), 0600); err != nil {
		t.Fatal(err)
	}
	lines, offset, err := readLogLines(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 1 || string(lines[0]) != "{\"a\":1}\n" || offset != int64(len("{\"a\":1}\n")) {
		t.Fatalf("lines=%d offset=%d", len(lines), offset)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.WriteString("}\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()
	lines, offset, err = readLogLines(path, offset)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 1 || string(lines[0]) != "{\"b\":2}\n" || offset != int64(len("{\"a\":1}\n{\"b\":2}\n")) {
		t.Fatalf("lines=%v offset=%d", lines, offset)
	}
}

func TestLogSiteMatching(t *testing.T) {
	sites := map[string]int64{"app.test": 1, "*.test": 2}
	for host, want := range map[string]int64{"APP.TEST:443": 1, "demo.test": 2, "deep.demo.test": 0, "test": 0} {
		if got := matchLogSite(host, sites); got != want {
			t.Fatalf("%s: got %d want %d", host, got, want)
		}
	}
}
