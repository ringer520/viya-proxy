package main

import (
	"strings"
	"testing"
)

// These cover the routing/streaming/maintenance fields added for path
// rewriting, SSE proxying and the 503 pause page. They are pure validation and
// rendering checks, so no database is involved.

func proxyHost() *Host {
	return &Host{Domain: "app.example.com", Type: "proxy", Upstream: "http://127.0.0.1:9000", TLSMode: "auto"}
}

func TestValidateHostStripPrefixRequiresPath(t *testing.T) {
	h := proxyHost()
	h.StripPrefix = true
	if err := validateHost(h); err == nil {
		t.Fatal("strip_prefix without an access path should be rejected")
	}
	h.Path = "/api"
	if err := validateHost(h); err != nil {
		t.Fatalf("strip_prefix with a path should pass: %v", err)
	}
}

func TestValidateHostRewriteToMustBeAbsolutePath(t *testing.T) {
	for _, bad := range []string{"new", "/has space", "/a?b", "/a#b", "/a\\b", "/" + strings.Repeat("x", 600)} {
		h := proxyHost()
		h.RewriteTo = bad
		if err := validateHost(h); err == nil {
			t.Fatalf("rewrite_to %q should be rejected", bad)
		}
	}
	h := proxyHost()
	h.RewriteTo = "/new-path"
	if err := validateHost(h); err != nil {
		t.Fatalf("a normal rewrite target should pass: %v", err)
	}
	if h.RewriteTo != "/new-path" {
		t.Fatalf("rewrite target was mutated to %q", h.RewriteTo)
	}
}

func TestValidateHostTimeouts(t *testing.T) {
	for _, bad := range []string{"abc", "0s", "-5s", "10"} {
		h := proxyHost()
		h.DialTimeout = bad
		if err := validateHost(h); err == nil {
			t.Fatalf("dial_timeout %q should be rejected", bad)
		}
		h = proxyHost()
		h.ResponseHeaderTimeout = bad
		if err := validateHost(h); err == nil {
			t.Fatalf("response_header_timeout %q should be rejected", bad)
		}
	}
	h := proxyHost()
	h.DialTimeout = "10s"
	h.ResponseHeaderTimeout = "2m"
	if err := validateHost(h); err != nil {
		t.Fatalf("valid durations should pass: %v", err)
	}
}

func TestValidateHostMaintenanceMessageLength(t *testing.T) {
	h := proxyHost()
	h.MaintenanceMessage = strings.Repeat("长", 501)
	if err := validateHost(h); err == nil {
		t.Fatal("a 501-rune maintenance message should be rejected")
	}
	h.MaintenanceMessage = "  系统升级中  "
	if err := validateHost(h); err != nil {
		t.Fatalf("a short message should pass: %v", err)
	}
	if h.MaintenanceMessage != "系统升级中" {
		t.Fatalf("message should be trimmed, got %q", h.MaintenanceMessage)
	}
}

func TestMaintenancePageEscapesOperatorMessage(t *testing.T) {
	body := maintenancePage(Host{MaintenanceMessage: `<script>alert(1)</script>`})
	if strings.Contains(body, "<script>") {
		t.Fatal("operator message must be HTML-escaped")
	}
	if !strings.Contains(body, "&lt;script&gt;") {
		t.Fatalf("escaped message missing from body: %s", body)
	}
	if !strings.Contains(body, "503") {
		t.Fatal("maintenance page should surface the 503 status")
	}
	// The default message is used when the operator leaves the field empty.
	if def := maintenancePage(Host{}); !strings.Contains(def, "维护") {
		t.Fatalf("default message missing: %s", def)
	}
}

// ---- file services (shared folder over HTTP and/or WebDAV) ----

func filesHost() *Host {
	return &Host{Domain: "share.example.com", Type: "files", TLSMode: "auto"}
}

func TestValidateHostFilesNeedsOneEntryPoint(t *testing.T) {
	h := filesHost()
	if err := validateHost(h); err == nil {
		t.Fatal("a file service with neither listing nor WebDAV should be rejected")
	}
	h.Browse = true
	if err := validateHost(h); err != nil {
		t.Fatalf("browse-only file service should pass: %v", err)
	}
	h.Browse = false
	h.WebDAV = true
	if err := validateHost(h); err != nil {
		t.Fatalf("WebDAV-only file service should pass: %v", err)
	}
}

func TestValidateHostFilesWriteNeedsWebDAV(t *testing.T) {
	h := filesHost()
	h.Browse = true
	h.WebDAVWrite = true
	if err := validateHost(h); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if h.WebDAVWrite {
		t.Fatal("write access must be dropped when WebDAV is off")
	}
}

func TestValidateHostFilesClearsProxyFields(t *testing.T) {
	h := filesHost()
	h.Browse = true
	h.Upstream = "http://127.0.0.1:9000"
	h.RedirectTarget = "https://elsewhere.example.com"
	h.SpaFallback = true
	if err := validateHost(h); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if h.Upstream != "" || h.RedirectTarget != "" || h.SpaFallback {
		t.Fatalf("file service must not keep proxy/redirect/SPA fields: %+v", h)
	}
}

func TestValidateHostStaticDropsWebDAV(t *testing.T) {
	h := &Host{Domain: "site.example.com", Type: "static", TLSMode: "auto", WebDAV: true, WebDAVWrite: true}
	if err := validateHost(h); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if h.WebDAV || h.WebDAVWrite {
		t.Fatal("static sites do not expose WebDAV")
	}
}
