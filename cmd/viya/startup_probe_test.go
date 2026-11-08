package main

import (
	"context"
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func TestFindLoopbackSelfProxies(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`CREATE TABLE proxy_hosts(domain TEXT, upstream TEXT, enabled INTEGER, type TEXT)`); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name        string
		listen      string
		extraAddrs  []string
		upstream    string
		enabled     int
		wantMatched int
	}{
		{"panel bound to loopback is fine", "127.0.0.1:8181", nil, "http://127.0.0.1:8181", 1, 0},
		{"panel bound to all interfaces is fine", "0.0.0.0:8181", nil, "http://127.0.0.1:8181", 1, 0},
		{"LAN bind plus loopback upstream is broken", "192.168.1.16:8181", nil, "http://127.0.0.1:8181", 1, 1},
		{"LAN bind with loopback companion is fine", "192.168.1.16:8181", []string{"127.0.0.1:8181", "[::1]:8181"}, "http://127.0.0.1:8181", 1, 0},
		{"IPv6 loopback companion also counts", "192.168.1.16:8181", []string{"[::1]:8181"}, "http://127.0.0.1:8181", 1, 0},
		{"LAN bind plus LAN upstream is correct", "192.168.1.16:8181", nil, "http://192.168.1.16:8181", 1, 0},
		{"a different port is not a self proxy", "192.168.1.16:8181", nil, "http://127.0.0.1:9000", 1, 0},
		{"disabled site is ignored", "192.168.1.16:8181", nil, "http://127.0.0.1:8181", 0, 0},
		{"bare loopback host without scheme", "192.168.1.16:8181", nil, "127.0.0.1:8181", 1, 1},
		{"empty listener list is harmless", "", nil, "http://127.0.0.1:8181", 1, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := db.Exec(`DELETE FROM proxy_hosts`); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(`INSERT INTO proxy_hosts(domain,upstream,enabled,type) VALUES(?,?,?,'proxy')`,
				"panel.example.com", tc.upstream, tc.enabled); err != nil {
				t.Fatal(err)
			}
			addrs := []string{}
			if tc.listen != "" {
				addrs = append(addrs, tc.listen)
			}
			addrs = append(addrs, tc.extraAddrs...)
			got := findLoopbackSelfProxies(context.Background(), db, addrs)
			if len(got) != tc.wantMatched {
				t.Fatalf("listen=%v upstream=%s enabled=%d -> %d matches, want %d (%v)",
					addrs, tc.upstream, tc.enabled, len(got), tc.wantMatched, got)
			}
			if tc.wantMatched == 1 && got[0].Upstream != tc.upstream {
				t.Fatalf("reported upstream %q, want %q", got[0].Upstream, tc.upstream)
			}
		})
	}
}
