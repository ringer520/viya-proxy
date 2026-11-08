package main

import (
	"log/slog"
	"net"
	"strings"
)

// defaultPanelListen is the safe default for an unconfigured binary: only the
// local machine can reach the panel. Installer profiles deliberately override
// it with a LAN address or 0.0.0.0. Keep the literal in one place so the CLI
// help, port-availability checks and the listener itself never disagree.
const defaultPanelListen = "127.0.0.1:8181"

// loopbackCompanions returns loopback addresses to bind alongside a panel that
// is bound to a concrete non-loopback IP. A socket bound to 192.168.1.16 never
// accepts packets sent to 127.0.0.1 (those arrive on the lo interface), which
// used to break local health checks and self-proxy sites -- or, worse, silently
// reach a second Viya process that happened to own the loopback port. Binding
// loopback inside the same process keeps one database and one set of accounts
// without widening exposure to any other interface.
func loopbackCompanions(addr string) []string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil || port == "" {
		return nil
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	if ip == nil {
		// A hostname may resolve to loopback or to a LAN address; guessing here
		// could bind an address the operator never intended.
		return nil
	}
	if ip.IsLoopback() || ip.IsUnspecified() {
		return nil
	}
	if ip.To4() == nil {
		return []string{net.JoinHostPort("::1", port), net.JoinHostPort("127.0.0.1", port)}
	}
	return []string{net.JoinHostPort("127.0.0.1", port), net.JoinHostPort("::1", port)}
}

// openPanelListeners binds the configured address first -- a failure there is
// fatal -- then adds loopback companions best-effort. An unavailable ::1 is
// normal on hosts with IPv6 disabled and must not abort startup; a missing
// 127.0.0.1 is worth a warning because another process may own that port.
func openPanelListeners(addr string) ([]net.Listener, []string, error) {
	primary, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, nil, err
	}
	listeners := []net.Listener{primary}
	bound := []string{addr}
	for _, extra := range loopbackCompanions(addr) {
		ln, e := net.Listen("tcp", extra)
		if e != nil {
			if strings.HasPrefix(extra, "[::1]") {
				slog.Debug("optional IPv6 loopback listener skipped", "address", extra, "error", e)
			} else {
				slog.Warn("loopback listener unavailable", "address", extra, "error", e,
					"hint", "another process may own this port; compare the startup log's data_dir/pid lines")
			}
			continue
		}
		listeners = append(listeners, ln)
		bound = append(bound, extra)
	}
	return listeners, bound, nil
}
