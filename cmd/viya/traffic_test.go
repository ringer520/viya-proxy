package main

import (
	"compress/gzip"
	"context"
	"database/sql"
	"fmt"
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
	if _, err = db.Exec(`PRAGMA foreign_keys=ON; CREATE TABLE proxy_hosts(id INTEGER PRIMARY KEY,domain TEXT,listener_id TEXT NOT NULL DEFAULT '');INSERT INTO proxy_hosts(id,domain) VALUES(1,'app.test'),(2,'*.test'),(3,'other.test');CREATE TABLE layer4_rules(id INTEGER PRIMARY KEY,name TEXT,listen_port INTEGER,protocol TEXT)`); err != nil {
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
	// The error logger repeats the access event for the same failed request.
	second = append(second, []byte(`{"logger":"http.log.error.viya_access_test","ts":1793318401,"status":500,"bytes_read":12,"size":34,"request":{"host":"app.test","uri":"/second"}}`+"\n")...)
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

func TestLayer4ConnectionStatsIngest(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if _, err = db.Exec(`PRAGMA foreign_keys=ON; CREATE TABLE proxy_hosts(id INTEGER PRIMARY KEY,domain TEXT,listener_id TEXT NOT NULL DEFAULT '');CREATE TABLE layer4_rules(id INTEGER PRIMARY KEY,name TEXT,listen_port INTEGER,protocol TEXT);INSERT INTO layer4_rules(id,name,listen_port,protocol) VALUES(1,'ssh',2222,'tcp'),(2,'dns',5353,'both')`); err != nil {
		t.Fatal(err)
	}
	a := &App{db: db, ctx: context.Background(), dataDir: t.TempDir()}
	if err = a.initTraffic(); err != nil {
		t.Fatal(err)
	}
	started := []byte(`{"level":"debug","ts":1793318400,"logger":"layer4","msg":"started handling connection","network":"tcp","local":"0.0.0.0:2222","remote":"192.0.2.1:5000"}` + "\n")
	stopped := []byte(`{"level":"debug","ts":1793318400,"logger":"layer4","msg":"stopped handling connection; connection stats","network":"tcp","local":"0.0.0.0:2222","remote":"192.0.2.1:5000","read":100,"written":250,"duration":1.5}` + "\n")
	if err = os.WriteFile(filepath.Join(a.dataDir, "l4-access.log"), append(started, stopped...), 0600); err != nil {
		t.Fatal(err)
	}
	if err = a.ingestTraffic(); err != nil {
		t.Fatal(err)
	}
	var connections, up, down int
	if err = db.QueryRow(`SELECT SUM(connections),SUM(upload),SUM(download) FROM layer4_traffic_hours WHERE rule_id=1`).Scan(&connections, &up, &down); err != nil {
		t.Fatal(err)
	}
	// Only the "stopped" line counts; the "started" line carries no byte stats.
	if connections != 1 || up != 100 || down != 250 {
		t.Fatalf("got connections=%d upload=%d download=%d, want 1/100/250", connections, up, down)
	}
	var zero int
	if err = db.QueryRow(`SELECT COUNT(*) FROM layer4_traffic_hours WHERE rule_id=2`).Scan(&zero); err != nil {
		t.Fatal(err)
	}
	if zero != 0 {
		t.Fatalf("udp rule 2 should have no rows, got %d", zero)
	}
}

// Regression for the v0.6.5 field failure: Caddy wrote the same startup line
// into both custom logs. A rotated l4 file and the active access.log therefore
// shared one generation; once the rotated file was marked complete, every
// subsequent HTTP access line was silently skipped.
func TestTrafficCursorIsPerFile(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if _, err = db.Exec(`PRAGMA foreign_keys=ON; CREATE TABLE proxy_hosts(id INTEGER PRIMARY KEY,domain TEXT,listener_id TEXT NOT NULL DEFAULT '');INSERT INTO proxy_hosts(id,domain) VALUES(1,'app.test');CREATE TABLE layer4_rules(id INTEGER PRIMARY KEY,name TEXT,listen_port INTEGER,protocol TEXT)`); err != nil {
		t.Fatal(err)
	}
	a := &App{db: db, ctx: context.Background(), dataDir: t.TempDir()}
	if err = a.initTraffic(); err != nil {
		t.Fatal(err)
	}
	startup := []byte(`{"level":"info","ts":1793894026.5,"logger":"tls.cache.maintenance","msg":"started background certificate maintenance"}` + "\n")
	event := func(ts int64) []byte {
		return []byte(fmt.Sprintf(`{"ts":%d,"status":200,"bytes_read":1,"size":2,"request":{"host":"app.test","uri":"/"}}`, ts) + "\n")
	}
	count := func() int {
		t.Helper()
		if err := a.ingestTraffic(); err != nil {
			t.Fatal(err)
		}
		var n int
		if err := db.QueryRow(`SELECT COALESCE(SUM(requests),0) FROM traffic_hours`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	path := filepath.Join(a.dataDir, "access.log")
	initial := append(append([]byte{}, startup...), event(1793318400)...)
	if err = os.WriteFile(path, initial, 0600); err != nil {
		t.Fatal(err)
	}
	if got := count(); got != 1 {
		t.Fatalf("initial ingest = %d, want 1", got)
	}
	archive, err := os.Create(filepath.Join(a.dataDir, "l4-access-rotated.log.gz"))
	if err != nil {
		t.Fatal(err)
	}
	z := gzip.NewWriter(archive)
	if _, err = z.Write(startup); err != nil {
		t.Fatal(err)
	}
	z.Close()
	archive.Close()
	if got := count(); got != 1 {
		t.Fatalf("after rotation = %d, want 1", got)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.Write(event(1793318401))
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	if got := count(); got != 2 {
		t.Fatalf("after append = %d, want 2 (active access.log was skipped)", got)
	}
}

// An old shared generation cursor can point past the end of the current file.
// Repairing it must not replay history that is already stored, while new lines
// appended afterwards must still be ingested.
func TestTrafficCursorPastEOFDoesNotReplayHistory(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if _, err = db.Exec(`PRAGMA foreign_keys=ON; CREATE TABLE proxy_hosts(id INTEGER PRIMARY KEY,domain TEXT,listener_id TEXT NOT NULL DEFAULT '');INSERT INTO proxy_hosts(id,domain) VALUES(1,'app.test');CREATE TABLE layer4_rules(id INTEGER PRIMARY KEY,name TEXT,listen_port INTEGER,protocol TEXT)`); err != nil {
		t.Fatal(err)
	}
	a := &App{db: db, ctx: context.Background(), dataDir: t.TempDir()}
	if err = a.initTraffic(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(a.dataDir, "access.log")
	line := []byte(`{"ts":1793318400,"status":200,"bytes_read":1,"size":2,"request":{"host":"app.test","uri":"/"}}` + "\n")
	if err = os.WriteFile(path, line, 0600); err != nil {
		t.Fatal(err)
	}
	count := func() int {
		t.Helper()
		if err := a.ingestTraffic(); err != nil {
			t.Fatal(err)
		}
		var n int
		if err := db.QueryRow(`SELECT COALESCE(SUM(requests),0) FROM traffic_hours`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if got := count(); got != 1 {
		t.Fatalf("initial ingest = %d, want 1", got)
	}
	if _, err = db.Exec(`UPDATE traffic_files SET offset=? WHERE path=?`, 1<<20, path); err != nil {
		t.Fatal(err)
	}
	if got := count(); got != 1 {
		t.Fatalf("cursor repair replayed %d extra requests", got-1)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.Write([]byte(`{"ts":1793318401,"status":200,"bytes_read":1,"size":2,"request":{"host":"app.test","uri":"/next"}}` + "\n"))
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	if got := count(); got != 2 {
		t.Fatalf("after append = %d, want 2", got)
	}
}

// The two custom logs must never receive the same entries again, otherwise
// traffic is counted twice and their first lines collide into one generation.
func TestLoggingConfigSplitsCustomLogs(t *testing.T) {
	dir := t.TempDir()
	logs := loggingConfig(dir, LogSettings{Level: "INFO", AccessMB: 100, ProgramMB: 20, Backups: 5, Days: 30})["logs"].(map[string]any)
	web := logs["viya_access"].(map[string]any)
	l4 := logs["layer4"].(map[string]any)
	include := func(m map[string]any) []string {
		got, ok := m["include"].([]string)
		if !ok {
			t.Fatalf("custom log has no include filter: %+v", m)
		}
		return got
	}
	if inc := include(web); len(inc) != 1 || inc[0] != "http.log.access" {
		t.Fatalf("web include = %v, want [http.log.access]", inc)
	}
	if inc := include(l4); len(inc) != 1 || inc[0] != "layer4" {
		t.Fatalf("l4 include = %v, want [layer4]", inc)
	}
	if got := web["writer"].(map[string]any)["filename"]; got != filepath.Join(dir, "access.log") {
		t.Fatalf("web writer filename = %v", got)
	}
	if got := l4["writer"].(map[string]any)["filename"]; got != filepath.Join(dir, "l4-access.log") {
		t.Fatalf("l4 writer filename = %v", got)
	}
	if _, ok := logs["default"].(map[string]any)["include"]; ok {
		t.Fatal("the default log must keep accepting the remaining entries")
	}
}

// Restoring a configuration point deletes proxy_hosts/layer4_rules, which
// cascades into the traffic tables. The L4 hourly rows must be captured and
// written back for rules that still exist instead of disappearing forever.
func TestRecoveryRuntimeKeepsLayer4Traffic(t *testing.T) {
	if !containsString(runtimeTables, "layer4_traffic_hours") {
		t.Fatal("layer4_traffic_hours is not captured before a configuration restore")
	}
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if _, err = db.Exec(`PRAGMA foreign_keys=OFF;
		CREATE TABLE layer4_rules(id INTEGER PRIMARY KEY);
		INSERT INTO layer4_rules(id) VALUES(1);
		CREATE TABLE layer4_traffic_hours(rule_id INTEGER NOT NULL,hour INTEGER NOT NULL,connections INTEGER NOT NULL,upload INTEGER NOT NULL,download INTEGER NOT NULL,PRIMARY KEY(rule_id,hour));
		INSERT INTO layer4_traffic_hours VALUES(1,100,3,10,20),(1,101,2,5,7),(99,100,1,1,1)`); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	dumps, err := captureTables(ctx, db, []string{"layer4_traffic_hours"})
	if err != nil {
		t.Fatal(err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = restoreRecoveryRuntime(ctx, tx, dumps); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var kept, orphaned int
	if err = db.QueryRow(`SELECT COUNT(*) FROM layer4_traffic_hours WHERE rule_id=1`).Scan(&kept); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(`SELECT COUNT(*) FROM layer4_traffic_hours WHERE rule_id=99`).Scan(&orphaned); err != nil {
		t.Fatal(err)
	}
	if kept != 2 || orphaned != 0 {
		t.Fatalf("kept=%d orphaned=%d, want 2/0", kept, orphaned)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
