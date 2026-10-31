package main

import (
	"encoding/json"
	"net"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseUpstream(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{name: "IPv4 with explicit port", input: "http://192.168.1.20:8080"},
		{name: "IPv6 with explicit port", input: "https://[fd00::20]:8443"},
		{name: "default HTTP port", input: "http://192.168.1.20"},
		{name: "default HTTPS port", input: "https://example.com"},
		{name: "credentials", input: "http://user:pass@192.168.1.20:8080", wantErr: true},
		{name: "path prefix", input: "http://192.168.1.20:8080/app", wantErr: true},
		{name: "unsupported scheme", input: "tcp://192.168.1.20:8080", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := parseUpstream(test.input)
			if (err != nil) != test.wantErr {
				t.Fatalf("parseUpstream(%q) error = %v, wantErr %v", test.input, err, test.wantErr)
			}
		})
	}
}

func TestTestUpstreamReachable(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	body, err := json.Marshal(map[string]string{"upstream": "http://" + listener.Addr().String()})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("POST", "/api/v1/proxy-hosts/test-upstream", strings.NewReader(string(body)))
	response := httptest.NewRecorder()
	(&App{}).testUpstream(response, request)
	if response.Code != 200 {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var result map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result["ok"] != true {
		t.Fatalf("expected successful probe, got %v", result)
	}
}
