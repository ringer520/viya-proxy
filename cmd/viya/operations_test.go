package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestAccessClientHonorsOnlyTrustedProxyChain(t *testing.T) {
	_, trusted, _ := net.ParseCIDR("127.0.0.0/8")
	a := &App{trustedProxies: []*net.IPNet{trusted}}
	r := httptest.NewRequest("GET", "http://panel.test/", nil)
	r.RemoteAddr = "203.0.113.9:1234"
	r.Header.Set("X-Forwarded-For", "192.168.1.1")
	if a.accessClientIP(r) != "203.0.113.9" {
		t.Fatal("trusted untrusted visitor header")
	}
	r.RemoteAddr = "127.0.0.1:2000"
	r.Header.Set("X-Forwarded-For", "192.168.1.1, 2607:8700:5500:2ee1::2")
	if a.accessClientIP(r) != "2607:8700:5500:2ee1::2" {
		t.Fatal("did not stop at first untrusted hop")
	}
	if !banContainsIP(IPBan{IP: "foreign"}, "2607:8700:5500:2ee1::2") || banContainsIP(IPBan{IP: "foreign"}, "192.168.1.16") {
		t.Fatal("region policy mismatch")
	}
	if !banContainsIP(IPBan{IP: "192.0.2.0/24"}, "192.0.2.5") {
		t.Fatal("CIDR match failed")
	}
}
func TestAccessGuardConfirmsOnlyAffectedDomainAndRestoresOldRule(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX validator; exercised on Debian")
	}
	a := stabilityApp(t)
	a.panelListen = "127.0.0.1:8181"
	a.httpListen = ":18080"
	a.adminListen = "127.0.0.1:2019"
	if e := a.initListenerSites(); e != nil {
		t.Fatal(e)
	}
	if e := a.initCertificateRequests(); e != nil {
		t.Fatal(e)
	}
	_, e := a.db.Exec(`INSERT INTO proxy_hosts(domain,upstream,tls_mode,type,listener_id) VALUES('panel.test','http://127.0.0.1:8181','none','proxy','http-main')`)
	if e != nil {
		t.Fatal(e)
	}
	a.caddyBin = filepath.Join(t.TempDir(), "validate")
	os.WriteFile(a.caddyBin, []byte("#!/bin/sh\nexit 0\n"), 0700)
	current := json.RawMessage(`{"apps":{}}`)
	engine := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			w.Write(current)
			return
		}
		if json.NewDecoder(r.Body).Decode(&current) != nil {
			w.WriteHeader(400)
		}
	}))
	defer engine.Close()
	a.adminURL = engine.URL
	save := func(method, path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.RemoteAddr = "192.168.1.8:1111"
		if method == "PUT" {
			r.SetPathValue("id", "1")
		}
		w := httptest.NewRecorder()
		a.saveIPBan(w, r)
		return w
	}
	w := save("POST", "/", `{"domain":"panel.test","ip":"foreign"}`)
	if w.Code != 409 {
		t.Fatal("risky save accepted without acknowledgement", w.Body.String())
	}
	w = save("POST", "/", `{"domain":"panel.test","ip":"foreign","confirm_risk":true}`)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if a.pendingAccessConfirmation(context.Background()) == nil {
		t.Fatal("no recovery guard")
	}
	confirm := httptest.NewRecorder()
	a.confirmAccessRule(confirm, httptest.NewRequest("POST", "http://192.168.1.16:8181/", nil))
	if confirm.Code != 400 {
		t.Fatal("LAN confirmed domain reachability")
	}
	confirm = httptest.NewRecorder()
	a.confirmAccessRule(confirm, httptest.NewRequest("POST", "http://panel.test/", nil))
	if confirm.Code != 200 {
		t.Fatal(confirm.Body.String())
	}
	w = save("PUT", "/", `{"domain":"panel.test","ip":"192.0.2.0/24","confirm_risk":true}`)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	a.db.Exec(`UPDATE access_rule_guards SET deadline=?`, time.Now().Unix()-1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a.ctx = ctx
	go a.watchAccessRules()
	deadline := time.Now().Add(8 * time.Second)
	for a.pendingAccessConfirmation(ctx) != nil && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	var ip string
	a.db.QueryRow(`SELECT ip FROM ip_bans WHERE id=1`).Scan(&ip)
	if ip != "foreign" || a.pendingAccessConfirmation(context.Background()) != nil {
		t.Fatal("did not restore original rule")
	}
}
func TestForwardAvailabilityAndRuntimeAreIndependentFromEnabled(t *testing.T) {
	a := stabilityApp(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	port := listener.Addr().(*net.TCPAddr).Port
	rule := Layer4Rule{ID: 1, Enabled: true, ListenAddress: "127.0.0.1", ListenPort: port, Protocol: "tcp", Upstream: "127.0.0.1:22"}
	if a.checkForwardSocket(context.Background(), 0, rule) == nil {
		t.Fatal("occupied port accepted")
	}
	engine := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"apps":{"layer4":{"servers":{}}}}`)) }))
	defer engine.Close()
	a.adminURL = engine.URL
	rules := []Layer4Rule{rule}
	a.forwardRuntimeStates(context.Background(), rules)
	if rules[0].Runtime.State != "pending" {
		t.Fatal("enabled rule falsely reported listening")
	}
}
func TestLogFiltersApplyBeforeLimitAndHideSecretQuery(t *testing.T) {
	a := stabilityApp(t)
	var body strings.Builder
	body.WriteString(`{"ts":100,"status":403,"request":{"host":"one.test","uri":"/?token=topsecret"}}` + "\n")
	for i := 0; i < 250; i++ {
		fmt.Fprintln(&body, `{"ts":200,"status":200,"request":{"host":"two.test","uri":"/"}}`)
	}
	os.WriteFile(filepath.Join(a.dataDir, "access.log"), []byte(body.String()), 0600)
	w := httptest.NewRecorder()
	a.recentLogs(w, httptest.NewRequest("GET", "/api/v1/logs?status=blocked&domain=one.test&from=50&to=150", nil))
	var events []map[string]any
	if json.Unmarshal(w.Body.Bytes(), &events) != nil || len(events) != 1 {
		t.Fatal("filter ran after limit", w.Body.String())
	}
	w = httptest.NewRecorder()
	a.recentLogs(w, httptest.NewRequest("GET", "/api/v1/logs?q=topsecret", nil))
	json.Unmarshal(w.Body.Bytes(), &events)
	if len(events) != 0 {
		t.Fatal("query search exposed a secret")
	}
	if _, err := parseLogQuery(httptest.NewRequest("GET", "/?from=200&to=100", nil)); err == nil {
		t.Fatal("invalid range accepted")
	}
}

func TestRuleMarkersDoNotLeakCookiesOrTriggerRecursiveBans(t *testing.T) {
	event := map[string]any{"status": float64(403), "resp_headers": map[string]any{"X-Viya-Access-Rule": []any{"4"}, "Set-Cookie": []any{"session=secret"}}, "request": map[string]any{"host": "panel.test", "remote_ip": "192.0.2.10"}}
	raw, _ := json.Marshal(event)
	a := &App{failCounts: map[string][]time.Time{}}
	a.banPolicyCache.Store(defaultBanPolicy())
	if a.observeAttack(raw) || len(a.failCounts) != 0 {
		t.Fatal("policy denial counted as a new attack")
	}
	sanitizeAccessEvent(event)
	if event["resp_headers"] != nil || event["access_rule_id"] != int64(4) {
		t.Fatal("cookie not removed or rule ID lost", event)
	}
}
func TestIPv6FlagsAndStageStorage(t *testing.T) {
	flags := parseIPv6Flags("24003200000000000000000000000001 02 40 00 a0 eth0\n24003200000000000000000000000002 02 40 00 01 eth0\n")
	if flags["2400:3200::1"]&0x20 == 0 || flags["2400:3200::2"]&1 == 0 {
		t.Fatal(flags)
	}
	a := stabilityApp(t)
	a.db.Exec(`INSERT INTO dns_providers(id,name,provider_type,credentials_ciphertext,credentials_nonce) VALUES(1,'test','cloudflare',x'00',x'00')`)
	_, err := a.db.Exec(`INSERT INTO ddns_records(id,name,dns_provider_id,zone,record_name,record_type) VALUES(1,'one',1,'example.com','@','A')`)
	if err != nil {
		t.Fatal(err)
	}
	a.setDDNSStage(context.Background(), 1, "verify")
	var stage string
	a.db.QueryRow(`SELECT last_stage FROM ddns_records WHERE id=1`).Scan(&stage)
	if stage != "verify" {
		t.Fatal(stage)
	}
}
func TestOperationSummaryExcludesPayload(t *testing.T) {
	r := httptest.NewRequest("DELETE", "/api/v1/layer4-rules/4", nil)
	r.SetPathValue("id", "4")
	ctx := requestOperation(r, "admin")
	value := ctx.Value(operationContextKey{}).(operationContext)
	if value.Summary != "删除端口转发 #4" || value.Actor != "admin" {
		t.Fatal(value)
	}
	a := stabilityApp(t)
	a.recordRevision(ctx, []byte(`{"secret":"hidden"}`), "applied", nil)
	var summary, actor string
	a.db.QueryRow(`SELECT summary,actor FROM config_revisions ORDER BY id DESC LIMIT 1`).Scan(&summary, &actor)
	if summary != value.Summary || actor != "admin" {
		t.Fatal("operation identity lost")
	}
}

func TestBusinessArchiveStripsAuditAccountIdentity(t *testing.T) {
	tables := []tableDump{{Name: "config_revisions", Columns: []string{"id", "summary", "actor"}, Rows: [][]configCell{{{Type: "integer", Value: "1"}, {Type: "text", Value: "修改站点"}, {Type: "text", Value: "private-admin"}}}}}
	stripArchiveActors(tables)
	if tables[0].Rows[0][2].Value != "" || tables[0].Rows[0][1].Value != "修改站点" {
		t.Fatal("business archive retained account identity or lost operation summary")
	}
}
