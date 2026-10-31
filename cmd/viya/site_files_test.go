package main

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveWithinRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "site")
	if err := os.MkdirAll(root, 0o750); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		rel     string
		wantErr bool
	}{
		{"", false},
		{".", false},
		{"index.html", false},
		{"assets/app.js", false},
		{"assets/../index.html", true}, // any .. is refused outright
		{"../escape", true},
		{"a/../../escape", true},
		{"..", true},
		{"..\\..\\win", true},
	} {
		full, err := resolveWithinRoot(root, tc.rel)
		if tc.wantErr {
			if err == nil {
				t.Fatalf("%q: expected error, got %q", tc.rel, full)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%q: unexpected error %v", tc.rel, err)
		}
		if full != root && !strings.HasPrefix(full, root+string(os.PathSeparator)) {
			t.Fatalf("%q: escaped root -> %q", tc.rel, full)
		}
	}
}

func TestValidateStaticRoot(t *testing.T) {
	good := filepath.Join(t.TempDir(), "nested", "site")
	sep := string(os.PathSeparator)
	for _, tc := range []struct {
		root    string
		wantErr bool
	}{
		{"", true},
		{"relative/path", true},
		{good, false},
		{good + sep + ".." + sep + "etc", true},
		{good + sep + "..", true},
		{good + "/../etc", true},
	} {
		err := validateStaticRoot(tc.root)
		if tc.wantErr && err == nil {
			t.Fatalf("%q: expected error", tc.root)
		}
		if !tc.wantErr && err != nil {
			t.Fatalf("%q: unexpected error %v", tc.root, err)
		}
	}
}

func buildZip(t *testing.T, entries map[string]string) *bytes.Buffer {
	t.Helper()
	buf := &bytes.Buffer{}
	zw := zip.NewWriter(buf)
	for name, body := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf
}

func TestExtractZipRejectsEscapes(t *testing.T) {
	dest := t.TempDir()
	for _, bad := range []string{"../evil.txt", "a/../../evil.txt", "dir/../../evil.txt"} {
		buf := buildZip(t, map[string]string{bad: "x"})
		if _, err := extractZip(buf, dest, 100, 1<<20); err == nil {
			t.Fatalf("entry %q should have been rejected", bad)
		}
	}
	entries, err := os.ReadDir(dest)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		t.Fatalf("unexpected file written: %s", entry.Name())
	}
}

func TestExtractZipLimits(t *testing.T) {
	dest := t.TempDir()
	buf := buildZip(t, map[string]string{"a.txt": strings.Repeat("x", 2048)})
	if _, err := extractZip(buf, dest, 100, 1024); err == nil {
		t.Fatal("expected size limit error")
	}
	buf = buildZip(t, map[string]string{"a.txt": "x", "b.txt": "y"})
	if _, err := extractZip(buf, dest, 1, 1<<20); err == nil {
		t.Fatal("expected entry count error")
	}
	buf = buildZip(t, map[string]string{"dir/ok.txt": "hello"})
	count, err := extractZip(buf, dest, 100, 1<<20)
	if err != nil {
		t.Fatalf("happy path failed: %v", err)
	}
	if count != 1 {
		t.Fatalf("want 1 extracted file, got %d", count)
	}
	body, err := os.ReadFile(filepath.Join(dest, "dir", "ok.txt"))
	if err != nil || string(body) != "hello" {
		t.Fatalf("extracted content wrong: %q %v", body, err)
	}
}

func TestValidateHostTypeBranching(t *testing.T) {
	absRoot := filepath.Join(t.TempDir(), "s")
	static := Host{Domain: "s.example.com", Type: "static", RootPath: absRoot}
	if err := validateHost(&static); err != nil {
		t.Fatalf("static: %v", err)
	}
	if static.Upstream != "" {
		t.Fatalf("static site should not carry an upstream: %q", static.Upstream)
	}
	redirect := Host{Domain: "r.example.com", Type: "redirect", RedirectTarget: "https://new.example.com"}
	if err := validateHost(&redirect); err != nil {
		t.Fatalf("redirect: %v", err)
	}
	if redirect.RedirectStatus != 301 {
		t.Fatalf("redirect status default want 301 got %d", redirect.RedirectStatus)
	}
	if redirect.RedirectTarget != "https://new.example.com" {
		t.Fatalf("target trimmed wrong: %q", redirect.RedirectTarget)
	}
	bad := Host{Domain: "x.example.com", Type: "static", RootPath: "relative"}
	if err := validateHost(&bad); err == nil {
		t.Fatal("relative static root must be rejected")
	}
	bad = Host{Domain: "x.example.com", Type: "redirect", RedirectTarget: "ftp://x"}
	if err := validateHost(&bad); err == nil {
		t.Fatal("non-http redirect target must be rejected")
	}
	bad = Host{Domain: "x.example.com", Type: "redirect", RedirectTarget: "https://x.example.com/?a=1"}
	if err := validateHost(&bad); err == nil {
		t.Fatal("redirect target with query must be rejected")
	}
	bad = Host{Domain: "x.example.com", Type: "wat"}
	if err := validateHost(&bad); err == nil {
		t.Fatal("unknown type must be rejected")
	}
	proxy := Host{Domain: "p.example.com", Type: "", Upstream: "http://127.0.0.1:9000"}
	if err := validateHost(&proxy); err != nil {
		t.Fatalf("proxy: %v", err)
	}
	if proxy.Type != "proxy" {
		t.Fatalf("empty type should default to proxy, got %q", proxy.Type)
	}
}
