package main

import (
	"context"
	"database/sql"
	"testing"
)

// Importing a configuration replaces proxy_hosts, and the foreign-key cascade
// deletes traffic_hours / site_error_hours with it. The import path must write
// the statistics back, keeping only rows whose site survived the import.
func TestImportRestoresHourlyStatistics(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if _, err = db.Exec(`PRAGMA foreign_keys=ON;
		CREATE TABLE proxy_hosts(id INTEGER PRIMARY KEY);
		INSERT INTO proxy_hosts(id) VALUES(1),(2);
		CREATE TABLE traffic_hours(site_id INTEGER NOT NULL REFERENCES proxy_hosts(id) ON DELETE CASCADE,hour INTEGER NOT NULL,requests INTEGER NOT NULL,errors INTEGER NOT NULL,upload INTEGER NOT NULL,download INTEGER NOT NULL,PRIMARY KEY(site_id,hour));
		INSERT INTO traffic_hours VALUES(1,100,7,0,10,20),(2,100,3,0,4,5);`); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	runtime, err := captureTables(ctx, db, []string{"traffic_hours"})
	if err != nil {
		t.Fatal(err)
	}
	// A configuration import replaces the site list: site 1 survives, site 2
	// does not, and replacing the table clears every counter with it.
	if _, err = db.Exec(`DELETE FROM proxy_hosts WHERE id=2; DELETE FROM traffic_hours;`); err != nil {
		t.Fatal(err)
	}
	var before int
	if err = db.QueryRow(`SELECT COUNT(*) FROM traffic_hours`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if before != 0 {
		t.Fatalf("the replaced table should be empty, still %d", before)
	}

	a := &App{db: db}
	if err = a.restoreRuntimeAfterImport(ctx, runtime); err != nil {
		t.Fatalf("restoreRuntimeAfterImport: %v", err)
	}
	var kept, orphaned int
	if err = db.QueryRow(`SELECT COUNT(*) FROM traffic_hours WHERE site_id=1`).Scan(&kept); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(`SELECT COUNT(*) FROM traffic_hours WHERE site_id=2`).Scan(&orphaned); err != nil {
		t.Fatal(err)
	}
	if kept != 1 {
		t.Fatalf("statistics for the surviving site were not restored: %d", kept)
	}
	if orphaned != 0 {
		t.Fatalf("statistics for a site removed by the import must be dropped: %d", orphaned)
	}
}
