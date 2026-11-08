package main

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// A WebDAV share that points at a system tree hands the whole host to any
// authenticated client, so the panel refuses the path instead of relying on the
// hardened unit to surface a confusing permission error later.
func TestShareRootRejectsSystemTrees(t *testing.T) {
	rejected := []string{
		"/", "/etc", "/etc/", "/home", "/root", "/usr", "/proc", "/sys",
		"/dev", "/boot", "/opt", "/var/lib/viya", "/var/lib/viya/", "/var/log",
	}
	for _, root := range rejected {
		if err := validateShareRoot(root); err == nil {
			t.Errorf("expected %q to be rejected", root)
		}
	}
	allowed := []string{"/var/lib/viya/sites/share", "/srv/data", "/mnt/nas-photos"}
	for _, root := range allowed {
		if err := validateShareRoot(root); err != nil {
			t.Errorf("expected %q to be allowed, got %v", root, err)
		}
	}
	// The login user's home holds SSH keys and cloud credentials, so the whole
	// directory must be refused even though /home itself is only the parent.
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		if err := validateShareRoot(home); err == nil {
			t.Errorf("expected home directory %q to be rejected", home)
		}
		if err := validateShareRoot(filepath.Join(home, "share")); err != nil {
			t.Errorf("expected a sub-directory of home to be allowed, got %v", err)
		}
	}
}

// Public read-only sharing is a real use case, but an anonymous writable share
// lets anyone who guesses the address upload and delete files.
func TestFileHostWriteRequiresAuth(t *testing.T) {
	// t.TempDir returns an absolute path on every platform, so this assertion
	// does not depend on Unix-only path handling in validateStaticRoot.
	base := Host{
		Domain:   "share.example.com",
		Type:     "files",
		RootPath: t.TempDir(),
		WebDAV:   true,
		Browse:   true,
	}

	anonymousWrite := base
	anonymousWrite.WebDAVWrite = true
	if err := validateHost(&anonymousWrite); err == nil {
		t.Fatal("expected an anonymous writable share to be rejected")
	}

	publicRead := base
	if err := validateHost(&publicRead); err != nil {
		t.Fatalf("public read-only share should stay allowed: %v", err)
	}

	authenticatedWrite := base
	authenticatedWrite.WebDAVWrite = true
	authenticatedWrite.BasicAuthEnabled = true
	authenticatedWrite.BasicAuthUser = "ryan"
	authenticatedWrite.BasicAuthPassword = "secret12345"
	if err := validateHost(&authenticatedWrite); err != nil {
		t.Fatalf("authenticated writable share should be allowed: %v", err)
	}
}

func TestTrafficWindowResolvesPresetsAndDays(t *testing.T) {
	cases := map[string]struct {
		window string
		hours  int
	}{
		"":           {"24h", 24},
		"24h":        {"24h", 24},
		"7d":         {"7d", 24 * 7},
		"30d":        {"30d", 24 * 30},
		"90d":        {"90d", 24 * 90},
		"days=14":    {"days=14", 24 * 14},
		"days=90":    {"days=90", 24 * 90},
		"days=0":     {"24h", 24},
		"days=91":    {"24h", 24},
		"days=abc":   {"24h", 24},
		"last-month": {"24h", 24},
	}
	for query, want := range cases {
		r := httptest.NewRequest("GET", "/api/v1/proxy-hosts/traffic?range="+query, nil)
		window, hours := trafficWindow(r)
		if window != want.window || hours != want.hours {
			t.Errorf("range=%q -> (%q, %d), want (%q, %d)", query, window, hours, want.window, want.hours)
		}
	}
}

func TestWebDAVSettingsPUTIsRetired(t *testing.T) {
	app := &App{}
	rec := httptest.NewRecorder()
	app.saveWebDAVSettings(rec, httptest.NewRequest("PUT", "/api/v1/webdav-settings", nil))
	if rec.Code != 410 {
		t.Fatalf("status = %d, want 410", rec.Code)
	}
	if rec.Header().Get("Deprecation") != "true" {
		t.Fatal("missing Deprecation header")
	}
}
