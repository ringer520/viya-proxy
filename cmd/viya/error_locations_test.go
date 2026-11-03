package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"
)

func TestErrorLocationsGroupsRetainedEventsBySiteAndStatus(t *testing.T) {
	db, e := sql.Open("sqlite", ":memory:")
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	_, e = db.Exec(`CREATE TABLE proxy_hosts(id INTEGER PRIMARY KEY,name TEXT,domain TEXT);CREATE TABLE site_access_events(id INTEGER PRIMARY KEY,site_id INTEGER,event TEXT);INSERT INTO proxy_hosts VALUES(1,'Files','files.test'),(2,'Photos','photo.test')`)
	if e != nil {
		t.Fatal(e)
	}
	now := time.Now().Unix()
	for _, v := range []struct {
		id     int
		status int
		at     int64
	}{{1, 502, now}, {1, 502, now}, {1, 404, now}, {2, 500, now}, {2, 200, now}, {2, 500, now - 48*3600}, {99, 503, now}} {
		data, _ := json.Marshal(map[string]any{"ts": v.at, "status": v.status, "request": map[string]string{"host": "files.test", "uri": "/example"}})
		if _, e = db.Exec(`INSERT INTO site_access_events(site_id,event) VALUES(?,?)`, v.id, string(data)); e != nil {
			t.Fatal(e)
		}
	}
	duplicate, _ := json.Marshal(map[string]any{"logger": "http.log.error.viya_access_test", "ts": now, "status": 502})
	if _, e = db.Exec(`INSERT INTO site_access_events(site_id,event) VALUES(1,?)`, string(duplicate)); e != nil {
		t.Fatal(e)
	}
	a := &App{db: db, ctx: context.Background()}
	r := httptest.NewRecorder()
	a.webErrorLocations(r, httptest.NewRequest("GET", "/api/v1/proxy-hosts/error-locations?range=24h", nil))
	if r.Code != 200 {
		t.Fatal(r.Code, r.Body.String())
	}
	var out struct {
		Groups     []errorLocation `json:"groups"`
		RecentOnly bool            `json:"recent_only"`
	}
	if e = json.Unmarshal(r.Body.Bytes(), &out); e != nil {
		t.Fatal(e)
	}
	if !out.RecentOnly || len(out.Groups) != 3 || out.Groups[0].SiteID != 1 || out.Groups[0].Status != 502 || out.Groups[0].Count != 2 || len(out.Groups[0].Events) != 2 {
		t.Fatalf("bad grouping: %+v", out)
	}
	r = httptest.NewRecorder()
	a.webErrorLocations(r, httptest.NewRequest("GET", "/api/v1/proxy-hosts/error-locations?range=7d", nil))
	if e = json.Unmarshal(r.Body.Bytes(), &out); e != nil {
		t.Fatal(e)
	}
	found := false
	for _, g := range out.Groups {
		if g.SiteID == 2 && g.Status == 500 {
			found = g.Count == 2
		}
	}
	if !found {
		t.Fatal("7 day range did not include older retained errors")
	}
}
