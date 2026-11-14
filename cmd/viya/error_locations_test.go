package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"
)

func TestErrorLocationsReadsAggregatesBySiteAndStatus(t *testing.T) {
	db, e := sql.Open("sqlite", ":memory:")
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	_, e = db.Exec(`CREATE TABLE proxy_hosts(id INTEGER PRIMARY KEY,name TEXT,domain TEXT);CREATE TABLE site_error_hours(site_id INTEGER NOT NULL,hour INTEGER NOT NULL,status INTEGER NOT NULL,count INTEGER NOT NULL,PRIMARY KEY(site_id,hour,status));CREATE TABLE traffic_hours(site_id INTEGER NOT NULL,hour INTEGER NOT NULL,requests INTEGER NOT NULL,errors INTEGER NOT NULL,upload INTEGER NOT NULL,download INTEGER NOT NULL,PRIMARY KEY(site_id,hour));INSERT INTO proxy_hosts VALUES(1,'Files','files.test'),(2,'Photos','photo.test')`)
	if e != nil {
		t.Fatal(e)
	}
	hour := time.Now().Unix() / 3600 * 3600
	for _, v := range []struct {
		site, status int
		hour         int64
		count        int
	}{{1, 502, hour, 2}, {1, 404, hour, 1}, {2, 500, hour, 2}, {2, 500, hour - 48*3600, 1}} {
		if _, e = db.Exec(`INSERT INTO site_error_hours(site_id,hour,status,count) VALUES(?,?,?,?)`, v.site, v.hour, v.status, v.count); e != nil {
			t.Fatal(e)
		}
	}
	if _, e = db.Exec(`INSERT INTO traffic_hours VALUES(1,?,10,3,0,0),(2,?,20,2,0,0)`, hour, hour); e != nil {
		t.Fatal(e)
	}
	a := &App{db: db, ctx: context.Background()}
	load := func(rangeName string) ([]errorLocation, int64) {
		t.Helper()
		r := httptest.NewRecorder()
		a.webErrorLocations(r, httptest.NewRequest("GET", "/api/v1/proxy-hosts/error-locations?range="+rangeName, nil))
		if r.Code != 200 {
			t.Fatal(r.Code, r.Body.String())
		}
		var out struct {
			Groups   []errorLocation `json:"groups"`
			Requests int64           `json:"requests"`
		}
		if e = json.Unmarshal(r.Body.Bytes(), &out); e != nil {
			t.Fatal(e)
		}
		return out.Groups, out.Requests
	}
	groups, requests := load("24h")
	if requests != 30 {
		t.Fatalf("requests = %d, want 30", requests)
	}
	if len(groups) != 3 || groups[0].SiteID != 1 || groups[0].Status != 502 || groups[0].Count != 2 || groups[0].Domain != "files.test" {
		t.Fatalf("bad grouping: %+v", groups)
	}
	groups, _ = load("7d")
	found := int64(0)
	for _, g := range groups {
		if g.SiteID == 2 && g.Status == 500 {
			found = g.Count
		}
	}
	if found != 3 {
		t.Fatalf("7 day range should include the older bucket, got %d", found)
	}
}
