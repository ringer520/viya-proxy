package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestVersionNewer(t *testing.T) {
	cases := []struct {
		current, candidate string
		want               bool
	}{
		{"v0.1.0", "v0.2.0", true},
		{"v0.1.0", "v0.1.1", true},
		{"v0.1.0", "v0.1.0", false},
		{"v0.2.0", "v0.1.0", false},
		{"v0.1.0", "v1.0.0", true},
		{"dev", "v0.1.0", true},
		{"v0.1.0", "dev", false},
		{"v0.1.0", "v0.1.0-rc1", false},
		{"v0.1.0-rc1", "v0.1.0", true},
		{"v0.9.9", "v0.10.0", true},
		{"v1.0.0", "v0.99.99", false},
	}
	for _, c := range cases {
		if got := versionNewer(c.current, c.candidate); got != c.want {
			t.Errorf("versionNewer(%q, %q) = %v, want %v", c.current, c.candidate, got, c.want)
		}
	}
}

func TestIsDockerized(t *testing.T) {
	// Just make sure it runs without panicking; the result depends on the
	// environment so we cannot assert a fixed value.
	_ = isDockerized()
}

func TestSwapEngineBinary(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "caddy")
	if err := os.WriteFile(path, []byte("old-engine"), 0755); err != nil {
		t.Fatal(err)
	}
	if warning := swapEngineBinary(path, []byte("new-engine")); warning != "" {
		t.Fatalf("successful swap returned a warning: %q", warning)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new-engine" {
		t.Fatalf("engine content = %q, want new-engine", got)
	}

	// A missing directory makes replaceExecutable fail. The failure must be
	// reported (not swallowed) and must not contain internal paths.
	missing := filepath.Join(dir, "no-such-dir", "caddy")
	warning := swapEngineBinary(missing, []byte("new-engine"))
	if warning != engineSwapWarning {
		t.Fatalf("failed swap warning = %q, want %q", warning, engineSwapWarning)
	}
	if warning == "" || len(warning) > 200 {
		t.Fatalf("warning must be a bounded user-facing message, got %q", warning)
	}
}

func TestUpdateWarningSurvivesProgressStages(t *testing.T) {
	setVersionUpdateWarning("")
	setVersionUpdateProgress("准备更新", 1, nil)
	if currentUpdateWarning() != "" {
		t.Fatal("a new update must start with a clean warning")
	}
	setVersionUpdateWarning(engineSwapWarning)
	setVersionUpdateProgress("正在重启", 97, nil)
	if got := currentUpdateWarning(); got != engineSwapWarning {
		t.Fatalf("warning lost after a progress update: %q", got)
	}
	setVersionUpdateProgress("正在重启", 99, nil)
	if got := currentUpdateWarning(); got != engineSwapWarning {
		t.Fatalf("warning lost after the final progress update: %q", got)
	}
	setVersionUpdateWarning("")
}

func TestReplaceExecutableFromReaderAndReleaseBases(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "viya-bin")
	if err := os.WriteFile(target, []byte("old"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := replaceExecutableFromReader(target, strings.NewReader("streamed-binary")); err != nil {
		t.Fatalf("replaceExecutableFromReader: %v", err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "streamed-binary" {
		t.Fatalf("content = %q, want streamed-binary", string(got))
	}

	t.Setenv("GH_PROXY", "https://mirror.example.com")
	bases := releaseDownloadBases("v0.6.10")
	if len(bases) < 2 || !strings.HasPrefix(bases[0], "https://mirror.example.com/https://github.com/") {
		t.Fatalf("unexpected releaseDownloadBases with GH_PROXY: %v", bases)
	}
}

func TestProbeReleaseMirrors(t *testing.T) {
	// Server 1: slow server (sleeps 400ms)
	s1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(400 * time.Millisecond)
		_, _ = w.Write([]byte("aaaa111122223333444455556666777788889999aaaabbbbccccddddeeeeffff  viya-linux-amd64.tar.gz\n"))
	}))
	defer s1.Close()

	// Server 2: fast server (responds in ~1ms)
	s2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("aaaa111122223333444455556666777788889999aaaabbbbccccddddeeeeffff  viya-linux-amd64.tar.gz\n"))
	}))
	defer s2.Close()

	// Server 3: broken server (returns 500)
	s3 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "server error", http.StatusInternalServerError)
	}))
	defer s3.Close()

	bases := []string{s1.URL, s2.URL, s3.URL}
	ordered, sum, format, err := probeReleaseMirrors(bases, "viya-linux-amd64.tar.gz", 2*time.Second)
	if err != nil {
		t.Fatalf("probeReleaseMirrors failed: %v", err)
	}
	if sum != "aaaa111122223333444455556666777788889999aaaabbbbccccddddeeeeffff" {
		t.Fatalf("unexpected sum: %q", sum)
	}
	if !format {
		t.Fatal("expected gotValidFormat to be true")
	}
	if len(ordered) != 3 {
		t.Fatalf("expected 3 bases, got %d", len(ordered))
	}
	if ordered[0] != s2.URL {
		t.Fatalf("expected fastest server s2 (%s) first, got %s", s2.URL, ordered[0])
	}
}

func TestDownloadLowSpeedCircuitBreaker(t *testing.T) {
	// Server that streams very slowly
	slowServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		chunk := make([]byte, 50)
		for i := 0; i < 40; i++ {
			_, _ = w.Write(chunk)
			if flusher != nil {
				flusher.Flush()
			}
			time.Sleep(200 * time.Millisecond)
		}
	}))
	defer slowServer.Close()

	dir := t.TempDir()
	dest := filepath.Join(dir, "test.tar.gz")

	// Case 1: hasFallback = true -> should abort with low speed error
	opts := downloadMonitorOptions{
		minSpeedBytesPerSec: 10 * 1024, // 10 KB/s
		warmupDuration:      1 * time.Second,
		stallTimeout:        5 * time.Second,
		hasFallback:         true,
	}
	_, err := downloadToFileWithProgressAndSHA256(slowServer.URL, dest, 10<<20, nil, opts)
	if err == nil || !strings.Contains(err.Error(), "download speed too low") {
		t.Fatalf("expected low speed abort error, got: %v", err)
	}

	// Case 2: hasFallback = false -> should NOT abort for low speed
	fastServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("hello viya update"))
	}))
	defer fastServer.Close()

	dest2 := filepath.Join(dir, "test2.tar.gz")
	optsNoFallback := downloadMonitorOptions{
		minSpeedBytesPerSec: 100 * 1024,
		warmupDuration:      1 * time.Second,
		stallTimeout:        5 * time.Second,
		hasFallback:         false,
	}
	sum, err := downloadToFileWithProgressAndSHA256(fastServer.URL, dest2, 10<<20, nil, optsNoFallback)
	if err != nil {
		t.Fatalf("expected success with hasFallback=false, got: %v", err)
	}
	if sum == "" {
		t.Fatal("expected non-empty sha256 sum")
	}
}

func TestDownloadStallTimeout(t *testing.T) {
	stallServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		time.Sleep(3 * time.Second)
	}))
	defer stallServer.Close()

	dir := t.TempDir()
	dest := filepath.Join(dir, "stall.tar.gz")
	opts := downloadMonitorOptions{
		minSpeedBytesPerSec: 0,
		warmupDuration:      1 * time.Second,
		stallTimeout:        500 * time.Millisecond,
		hasFallback:         false,
	}
	_, err := downloadToFileWithProgressAndSHA256(stallServer.URL, dest, 10<<20, nil, opts)
	if err == nil || !strings.Contains(err.Error(), "download stalled") {
		t.Fatalf("expected stalled error, got: %v", err)
	}
}
