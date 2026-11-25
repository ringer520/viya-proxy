package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// A cloned copy of a site on the plain HTTP listener carries tls_mode=none.
// Automatic HTTPS is a per-server setting, so that copy must not suppress
// certificate issuance for the same domain on the HTTPS listener.
func TestPlainHTTPCloneDoesNotDisableHTTPSAutomation(t *testing.T) {
	a := stabilityApp(t)
	if err := a.initListenerSites(); err != nil {
		t.Fatal(err)
	}
	if err := a.initCertificateRequests(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	seed := []Host{
		{
			Name: "edge", Domain: "app.example.com", ListenerID: "https-main",
			Upstream: "http://127.0.0.1:8001", Enabled: true, TLSMode: "auto", Type: "proxy",
			CustomHeaders: map[string]string{}, IPAllowlist: []string{}, IPDenylist: []string{}, AllowedMethods: []string{},
		},
		{
			Name: "edge-http", Domain: "app.example.com", ListenerID: "http-main",
			Upstream: "http://127.0.0.1:8001", Enabled: true, TLSMode: "none", Type: "proxy",
			CustomHeaders: map[string]string{}, IPAllowlist: []string{}, IPDenylist: []string{}, AllowedMethods: []string{},
		},
	}
	for _, h := range seed {
		if _, err := a.db.ExecContext(ctx, hostInsertSQL(), hostWriteValues(h)...); err != nil {
			t.Fatalf("insert %s: %v", h.Name, err)
		}
	}

	raw, err := a.compileConfig(ctx)
	if err != nil {
		t.Fatalf("compileConfig: %v", err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("parse config: %v", err)
	}
	servers, _ := cfg["apps"].(map[string]any)["http"].(map[string]any)["servers"].(map[string]any)
	if len(servers) == 0 {
		t.Fatalf("no servers compiled: %s", raw)
	}

	seenHTTPS := false
	for name, value := range servers {
		server, _ := value.(map[string]any)
		listen, _ := server["listen"].([]any)
		if len(listen) == 0 {
			continue
		}
		addr, _ := listen[0].(string)
		if !strings.HasSuffix(addr, ":443") {
			continue
		}
		seenHTTPS = true
		auto, ok := server["automatic_https"].(map[string]any)
		if !ok {
			t.Fatalf("server %s (%s) has no automatic_https", name, addr)
		}
		skip, _ := auto["skip"].([]any)
		for _, entry := range skip {
			if domain, _ := entry.(string); domain == "app.example.com" {
				t.Fatalf("server %s (%s) skips automatic HTTPS for a domain that is only plain HTTP on another listener", name, addr)
			}
		}
	}
	if !seenHTTPS {
		t.Fatalf("expected an HTTPS server in the compiled config: %s", raw)
	}
}
