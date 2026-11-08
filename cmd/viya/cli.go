package main

import (
	"fmt"
	"io"
	"os"
	"runtime"
)

type cliAction int

const (
	cliStart cliAction = iota
	cliVersion
	cliHelp
)

const cliUsage = `Viya - Caddy reverse proxy manager

Usage:
  viya              Start the panel (no arguments)
  viya version      Print build information and exit
  viya --version
  viya -v
  viya help         Show this help

Configuration is provided through environment variables:
  VIYA_LISTEN           Panel bind address (default 127.0.0.1:8181)
  VIYA_DATA_DIR         Data directory (default ./data, resolved from the working directory)
  VIYA_CADDY_ADMIN      Caddy admin API (default http://127.0.0.1:2019)
  VIYA_CADDY_BIN        Caddy binary path
  VIYA_MASTER_KEY_FILE  Master key path
  VIYA_TRUSTED_PROXIES  Comma-separated trusted proxy networks

When VIYA_LISTEN names a concrete non-loopback IP (for example a LAN address
written by the systemd installer), Viya also binds 127.0.0.1 on the same port
in this same process, plus ::1 when IPv6 is available. Local health checks and
self-proxy sites then reach the same panel as LAN clients; no other interface
is exposed. Wildcard (0.0.0.0) and loopback binds already cover loopback, so no
extra listener is created.

The data directory and all bound addresses are printed at startup. Two
processes using different data directories expose two unrelated panels, so
always check those lines before assuming a mismatched login is a cache or
session problem.
`

// parseCLIArgs is a pure function so the argument contract stays testable.
// Previously every argument was ignored: an operator running `viya version`
// silently launched a second full server (with its own data directory and
// bind address) instead of printing a version.
func parseCLIArgs(args []string) (cliAction, error) {
	if len(args) == 0 {
		return cliStart, nil
	}
	switch args[0] {
	case "version", "--version", "-v":
		if len(args) > 1 {
			return cliStart, fmt.Errorf("version takes no arguments, got %q", args[1])
		}
		return cliVersion, nil
	case "help", "--help", "-h":
		if len(args) > 1 {
			return cliStart, fmt.Errorf("help takes no arguments, got %q", args[1])
		}
		return cliHelp, nil
	default:
		return cliStart, fmt.Errorf("unknown argument %q", args[0])
	}
}

func printVersion(w io.Writer) {
	fmt.Fprintf(w, "Viya %s\n", version)
	fmt.Fprintf(w, "  commit:         %s\n", buildCommit)
	fmt.Fprintf(w, "  built:          %s\n", buildTime)
	fmt.Fprintf(w, "  source:         %s\n", buildSource)
	fmt.Fprintf(w, "  schema version: %d\n", schemaVersion)
	fmt.Fprintf(w, "  runtime:        %s %s/%s\n", runtime.Version(), runtime.GOOS, runtime.GOARCH)
}

// runCLI handles the non-server invocations. It returns true when main must
// exit without starting the panel.
func runCLI(args []string) bool {
	action, err := parseCLIArgs(args)
	if err == nil {
		switch action {
		case cliStart:
			return false
		case cliVersion:
			printVersion(os.Stdout)
			return true
		case cliHelp:
			fmt.Fprint(os.Stdout, cliUsage)
			return true
		}
	}
	fmt.Fprintf(os.Stderr, "viya: %v\nRun 'viya help' for usage.\n", err)
	os.Exit(2)
	return true
}
