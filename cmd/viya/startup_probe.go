package main

import (
	"context"
	"database/sql"
	"log/slog"
	"net"
	"net/url"
	"strings"
)

// warnOnSharedPanelPort catches the case that bit us in production: binding
// 127.0.0.1:8181 and 192.168.1.16:8181 does not conflict at the OS level, so
// two Viya processes can coexist while serving completely different panels
// (different data directories, accounts and sites). A wildcard probe fails if
// any address already owns the port; the warning names the exact consequence
// instead of leaving the operator to guess why logins look inconsistent.
//
// It MUST run before any panel listener is bound: this process's own loopback
// companions are created afterwards and are not "another instance".
func warnOnSharedPanelPort(addr string) {
	_, port, err := net.SplitHostPort(addr)
	if err != nil || port == "" {
		return
	}
	probe, err := net.Listen("tcp", net.JoinHostPort("", port))
	if err != nil {
		slog.Warn("panel port is already bound on another address",
			"port", port, "this_listener", addr,
			"hint", "another Viya instance may be running with a different data directory; compare the startup log's data_dir/pid lines and stop the extra process")
		return
	}
	probe.Close()
}

type loopbackSelfProxy struct {
	Domain   string
	Upstream string
}

// findLoopbackSelfProxies lists enabled proxy sites that forward to the panel
// port on a loopback address while no listener of this process can be reached
// that way. Such a site either returns 502 or, worse, silently reaches a
// different process that happens to own the loopback port (which is exactly
// how two "different panels" appeared). Passing every bound address matters:
// since v0.6.3 a LAN-bound panel also owns loopback, so the warning is no
// longer applicable.
func findLoopbackSelfProxies(ctx context.Context, db *sql.DB, listenAddrs []string) []loopbackSelfProxy {
	if len(listenAddrs) == 0 {
		return nil
	}
	for _, bound := range listenAddrs {
		host, _, err := net.SplitHostPort(bound)
		if err != nil {
			continue
		}
		if ip := net.ParseIP(strings.Trim(host, "[]")); ip != nil && (ip.IsLoopback() || ip.IsUnspecified()) {
			return nil
		}
	}
	panelHost, panelPort, err := net.SplitHostPort(listenAddrs[0])
	if err != nil || panelPort == "" || strings.Trim(panelHost, "[]") == "" {
		return nil
	}
	rows, err := db.QueryContext(ctx, `SELECT domain,upstream FROM proxy_hosts WHERE enabled=1 AND (type='proxy' OR type='')`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := []loopbackSelfProxy{}
	for rows.Next() {
		var domain, upstream string
		if rows.Scan(&domain, &upstream) != nil {
			continue
		}
		raw := strings.TrimSpace(upstream)
		if raw == "" {
			continue
		}
		if !strings.Contains(raw, "://") {
			raw = "http://" + raw
		}
		u, parseErr := url.Parse(raw)
		if parseErr != nil || u.Host == "" {
			continue
		}
		port := u.Port()
		if port == "" {
			if u.Scheme == "https" {
				port = "443"
			} else {
				port = "80"
			}
		}
		if port != panelPort || !isLoopbackAddress(u.Hostname()) {
			continue
		}
		out = append(out, loopbackSelfProxy{Domain: domain, Upstream: upstream})
	}
	if rows.Err() != nil {
		return nil
	}
	return out
}

func warnLoopbackSelfProxies(ctx context.Context, db *sql.DB, listenAddrs []string) {
	if len(listenAddrs) == 0 {
		return
	}
	_, port, _ := net.SplitHostPort(listenAddrs[0])
	for _, site := range findLoopbackSelfProxies(ctx, db, listenAddrs) {
		slog.Warn("site proxies the panel via loopback, but this process does not listen on loopback",
			"domain", site.Domain, "upstream", site.Upstream, "panel_listen", listenAddrs[0],
			"hint", "set the site upstream to http://"+listenAddrs[0]+", or bind VIYA_LISTEN to 0.0.0.0:"+port)
	}
}
