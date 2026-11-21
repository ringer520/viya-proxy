package main

import (
	"context"
	"strings"
	"testing"
)

// sampleHost fills every writable column with a non-zero value so a column
// dropped from either the SELECT list or the INSERT cannot go unnoticed.
func sampleHost() Host {
	providerID := int64(7)
	certID := int64(9)
	return Host{
		Name:                  "roundtrip",
		Domain:                "roundtrip.example.com",
		ListenerID:            "https:443",
		Upstream:              "http://127.0.0.1:9000",
		Enabled:               true,
		TLSMode:               "none",
		Path:                  "/app",
		IconColor:             "#2563eb",
		DNSProviderID:         &providerID,
		CustomCertificateID:   &certID,
		CustomHeaders:         map[string]string{"X-Test": "1"},
		BasicAuthEnabled:      true,
		BasicAuthUser:         "user",
		PasswordHash:          "$2a$10$abcdefghijklmnopqrstuv",
		IPAllowlist:           []string{"10.0.0.0/8"},
		IPDenylist:            []string{"192.0.2.0/24"},
		RateLimitRequests:     120,
		RateLimitWindow:       "1m",
		Type:                  "proxy",
		RootPath:              "",
		SpaFallback:           true,
		Browse:                true,
		WebDAV:                true,
		WebDAVWrite:           true,
		RedirectTarget:        "",
		RedirectStatus:        0,
		RedirectKeepPath:      true,
		SecurityHeaders:       SecurityHeaders{FrameOptions: "DENY", ReferrerPolicy: "no-referrer"},
		MaxBodyBytes:          1 << 20,
		AllowedMethods:        []string{"GET", "POST"},
		HideServer:            true,
		StripPrefix:           true,
		RewriteTo:             "/target",
		Streaming:             true,
		UpstreamHost:          "{http.request.hostport}",
		UpstreamTLSServerName: "origin.example.com",
		UpstreamSkipTLSVerify: true,
		DialTimeout:           "5s",
		ResponseHeaderTimeout: "30s",
		Maintenance:           true,
		MaintenanceMessage:    "维护中",
	}
}

func TestHostWriteReadRoundTrip(t *testing.T) {
	a := stabilityApp(t)
	ctx := context.Background()
	if err := a.initListenerSites(); err != nil {
		t.Fatal(err)
	}
	if err := a.initCertificateRequests(); err != nil {
		t.Fatal(err)
	}
	// The nullable columns are real foreign keys; seed the rows they point at.
	if _, err := a.db.ExecContext(ctx, `
		INSERT INTO dns_providers(id,name,provider_type,credentials_ciphertext,credentials_nonce)
			VALUES(7,'provider','cloudflare',x'00',x'00');
		INSERT INTO custom_certificates(id,name,cert_path,key_path,dns_names_json,not_after)
			VALUES(9,'cert','/tmp/cert.pem','/tmp/key.pem','[]','2030-01-01');`); err != nil {
		t.Fatal(err)
	}
	want := sampleHost()

	res, err := a.db.ExecContext(ctx, hostInsertSQL(), hostWriteValues(want)...)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	read := func() *Host {
		row := a.db.QueryRowContext(ctx, `SELECT `+hostReadColumns+` FROM proxy_hosts WHERE id=?`, id)
		got, err := scanHost(row)
		if err != nil {
			t.Fatalf("scan: %v", err)
		}
		return got
	}
	assertSame := func(stage string, got *Host) {
		if got.Name != want.Name || got.Domain != want.Domain || got.ListenerID != want.ListenerID ||
			got.Upstream != want.Upstream || got.TLSMode != want.TLSMode || got.Path != want.Path ||
			got.IconColor != want.IconColor || !got.Enabled || !got.SpaFallback || !got.Browse ||
			!got.WebDAV || !got.WebDAVWrite || !got.RedirectKeepPath || !got.HideServer ||
			!got.StripPrefix || !got.Streaming || !got.UpstreamSkipTLSVerify {
			t.Fatalf("%s: scalar or boolean column lost: %+v", stage, got)
		}
		if got.RewriteTo != want.RewriteTo || got.MaintenanceMessage != want.MaintenanceMessage ||
			got.RateLimitWindow != want.RateLimitWindow || got.DialTimeout != want.DialTimeout ||
			got.ResponseHeaderTimeout != want.ResponseHeaderTimeout ||
			got.UpstreamHost != want.UpstreamHost || got.UpstreamTLSServerName != want.UpstreamTLSServerName ||
			got.PasswordHash != want.PasswordHash || got.BasicAuthUser != want.BasicAuthUser {
			t.Fatalf("%s: string column lost: %+v", stage, got)
		}
		if got.RateLimitRequests != want.RateLimitRequests || got.MaxBodyBytes != want.MaxBodyBytes {
			t.Fatalf("%s: numeric column lost: %+v", stage, got)
		}
		if got.DNSProviderID == nil || *got.DNSProviderID != *want.DNSProviderID ||
			got.CustomCertificateID == nil || *got.CustomCertificateID != *want.CustomCertificateID {
			t.Fatalf("%s: nullable column lost: %+v", stage, got)
		}
		if got.CustomHeaders["X-Test"] != "1" ||
			len(got.IPAllowlist) != 1 || got.IPAllowlist[0] != "10.0.0.0/8" ||
			len(got.IPDenylist) != 1 || got.IPDenylist[0] != "192.0.2.0/24" ||
			len(got.AllowedMethods) != 2 ||
			got.SecurityHeaders.FrameOptions != "DENY" || got.SecurityHeaders.ReferrerPolicy != "no-referrer" {
			t.Fatalf("%s: JSON column lost: %+v", stage, got)
		}
		if !got.BasicAuthEnabled {
			t.Fatalf("%s: BasicAuthEnabled not derived from the stored hash", stage)
		}
	}

	inserted := read()
	if !inserted.Maintenance {
		t.Fatal("insert: maintenance flag lost")
	}
	assertSame("insert", inserted)

	// Update through the shared statement: every column must survive again,
	// and the optimistic version must advance.
	want.Name = "roundtrip-renamed"
	want.Maintenance = false
	if _, err := a.db.ExecContext(ctx, hostUpdateSQL(), append(hostWriteValues(want), id, int64(1))...); err != nil {
		t.Fatalf("update: %v", err)
	}
	got := read()
	assertSame("update", got)
	if got.Name != "roundtrip-renamed" || got.Maintenance {
		t.Fatalf("update did not apply: %+v", got)
	}
	if got.Version != 2 {
		t.Fatalf("version did not advance: %d", got.Version)
	}
}

func TestCompileConfigUpstreamSkipTLSVerify(t *testing.T) {
	a := stabilityApp(t)
	ctx := context.Background()
	if err := a.initListenerSites(); err != nil {
		t.Fatal(err)
	}
	if err := a.initCertificateRequests(); err != nil {
		t.Fatal(err)
	}
	h := Host{
		Name:                  "pve",
		Domain:                "pve.example.com",
		ListenerID:            "http-main",
		Upstream:              "https://192.168.1.100:8006",
		Enabled:               true,
		TLSMode:               "none",
		Type:                  "proxy",
		UpstreamSkipTLSVerify: true,
		CustomHeaders:         map[string]string{},
		IPAllowlist:           []string{},
		IPDenylist:            []string{},
		AllowedMethods:        []string{},
	}
	if _, err := a.db.ExecContext(ctx, hostInsertSQL(), hostWriteValues(h)...); err != nil {
		t.Fatalf("insert host: %v", err)
	}
	cfg, err := a.compileConfig(ctx)
	if err != nil {
		t.Fatalf("compileConfig: %v", err)
	}
	if !strings.Contains(string(cfg), `"insecure_skip_verify":true`) {
		t.Fatalf("expected insecure_skip_verify in compiled config, got: %s", string(cfg))
	}

	if _, err := a.db.ExecContext(ctx, `UPDATE proxy_hosts SET upstream_skip_tls_verify=0 WHERE domain='pve.example.com'`); err != nil {
		t.Fatalf("update host: %v", err)
	}
	cfg, err = a.compileConfig(ctx)
	if err != nil {
		t.Fatalf("compileConfig after disable: %v", err)
	}
	if strings.Contains(string(cfg), `"insecure_skip_verify"`) {
		t.Fatalf("unexpected insecure_skip_verify when disabled: %s", string(cfg))
	}
}

func TestCompileConfigRouteOrderAndPolicyDedup(t *testing.T) {
	// Verify routing order helper directly (exact domain before wildcard,
	// longer path before shorter path).
	ordered := []Host{
		{ID: 1, Domain: "*.example.com", Path: ""},
		{ID: 2, Domain: "app.example.com", Path: ""},
		{ID: 3, Domain: "app.example.com", Path: "/api"},
		{ID: 4, Domain: "app.example.com", Path: "/api/v2"},
	}
	sortHostsForRouting(ordered)
	if ordered[0].Path != "/api/v2" || ordered[1].Path != "/api" || ordered[2].Domain != "app.example.com" || ordered[3].Domain != "*.example.com" {
		t.Fatalf("unexpected sortHostsForRouting order: %+v", ordered)
	}

	a := stabilityApp(t)
	ctx := context.Background()
	if err := a.initListenerSites(); err != nil {
		t.Fatal(err)
	}
	if err := a.initCertificateRequests(); err != nil {
		t.Fatal(err)
	}
	seed := []Host{
		{
			Name: "wildcard", Domain: "*.example.com", ListenerID: "http-main",
			Upstream: "http://127.0.0.1:8003", Enabled: true, TLSMode: "none", Type: "proxy",
			CustomHeaders: map[string]string{}, IPAllowlist: []string{}, IPDenylist: []string{}, AllowedMethods: []string{},
		},
		{
			Name: "http-app", Domain: "app.example.com", ListenerID: "http-main", Path: "",
			Upstream: "http://127.0.0.1:8001", Enabled: true, TLSMode: "http-01", Type: "proxy",
			CustomHeaders: map[string]string{}, IPAllowlist: []string{}, IPDenylist: []string{}, AllowedMethods: []string{},
		},
		{
			Name: "https-app", Domain: "app.example.com", ListenerID: "https-main", Path: "/api",
			Upstream: "http://127.0.0.1:8002", Enabled: true, TLSMode: "http-01", Type: "proxy",
			CustomHeaders: map[string]string{}, IPAllowlist: []string{}, IPDenylist: []string{}, AllowedMethods: []string{},
		},
	}
	for _, h := range seed {
		if _, err := a.db.ExecContext(ctx, hostInsertSQL(), hostWriteValues(h)...); err != nil {
			t.Fatalf("insert %s: %v", h.Name, err)
		}
	}
	cfgBytes, err := a.compileConfig(ctx)
	if err != nil {
		t.Fatalf("compileConfig: %v", err)
	}
	cfg := string(cfgBytes)
	exactIdx := strings.Index(cfg, "127.0.0.1:8001")
	wildIdx := strings.Index(cfg, "127.0.0.1:8003")
	if exactIdx < 0 || wildIdx < 0 {
		t.Fatalf("missing expected upstreams in config: %s", cfg)
	}
	if exactIdx >= wildIdx {
		t.Fatalf("expected exact domain route before wildcard on same listener: exact=%d wildcard=%d", exactIdx, wildIdx)
	}
	if count := strings.Count(cfg, `"subjects":["app.example.com"]`); count != 1 {
		t.Fatalf("expected 1 TLS policy for app.example.com, got %d", count)
	}
}

func TestCompileConfigUpstreamHostAndSNI(t *testing.T) {
	a := stabilityApp(t)
	ctx := context.Background()
	if err := a.initListenerSites(); err != nil {
		t.Fatal(err)
	}
	if err := a.initCertificateRequests(); err != nil {
		t.Fatal(err)
	}
	h := Host{
		Name:                  "cockpit",
		Domain:                "cockpit.example.com",
		ListenerID:            "http-main",
		Upstream:              "https://192.168.1.50:9090",
		Enabled:               true,
		TLSMode:               "none",
		Type:                  "proxy",
		UpstreamHost:          "{http.request.hostport}",
		UpstreamTLSServerName: "cockpit.internal",
		CustomHeaders:         map[string]string{},
		IPAllowlist:           []string{},
		IPDenylist:            []string{},
		AllowedMethods:        []string{},
	}
	if _, err := a.db.ExecContext(ctx, hostInsertSQL(), hostWriteValues(h)...); err != nil {
		t.Fatalf("insert host: %v", err)
	}
	cfgBytes, err := a.compileConfig(ctx)
	if err != nil {
		t.Fatalf("compileConfig: %v", err)
	}
	cfg := string(cfgBytes)
	if !strings.Contains(cfg, `"headers":{"request":{"set":{"Host":["{http.request.hostport}"]}}}`) {
		t.Fatalf("expected upstream Host header override in compiled config, got: %s", cfg)
	}
	if !strings.Contains(cfg, `"server_name":"cockpit.internal"`) {
		t.Fatalf("expected custom TLS server_name in compiled config, got: %s", cfg)
	}

	// Clearing both fields must revert to default behavior (no headers override, server_name falls back to upstream hostname).
	if _, err := a.db.ExecContext(ctx, `UPDATE proxy_hosts SET upstream_host='', upstream_tls_server_name='' WHERE domain='cockpit.example.com'`); err != nil {
		t.Fatalf("clear overrides: %v", err)
	}
	cfgBytes, err = a.compileConfig(ctx)
	if err != nil {
		t.Fatalf("compileConfig after clear: %v", err)
	}
	cfg = string(cfgBytes)
	if strings.Contains(cfg, `"set":{"Host"`) {
		t.Fatalf("unexpected Host header override after clearing: %s", cfg)
	}
	if !strings.Contains(cfg, `"server_name":"192.168.1.50"`) {
		t.Fatalf("expected default server_name 192.168.1.50 after clearing, got: %s", cfg)
	}
}
