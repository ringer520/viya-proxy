package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
