package main

import (
	"archive/zip"
	"bytes"
	"database/sql"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newStaticFilesApp(t *testing.T, domain string) (*App, *sql.DB, string) {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`CREATE TABLE proxy_hosts(id INTEGER PRIMARY KEY, domain TEXT NOT NULL, type TEXT NOT NULL DEFAULT 'proxy', root_path TEXT NOT NULL DEFAULT '')`); err != nil {
		t.Fatal(err)
	}
	dataDir := t.TempDir()
	siteRoot := filepath.Join(dataDir, "sites", domain)
	if err := os.MkdirAll(siteRoot, 0o750); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO proxy_hosts(id,domain,type,root_path) VALUES(1,?,'static',?)`, domain, siteRoot); err != nil {
		t.Fatal(err)
	}
	return &App{db: db, dataDir: dataDir}, db, siteRoot
}

// uploadRequest builds a multipart request pairing each file with its relative
// path, mirroring how the drawer sends directory uploads.
func uploadRequest(t *testing.T, files map[string]string) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for rel, content := range files {
		fw, err := mw.CreateFormFile("files", filepath.Base(filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
		if err := mw.WriteField("paths", rel); err != nil {
			t.Fatal(err)
		}
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", "/api/v1/proxy-hosts/1/files/upload", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.SetPathValue("id", "1")
	return req
}

func TestUploadPreservesDirectoryStructure(t *testing.T) {
	app, db, root := newStaticFilesApp(t, "example.com")
	defer db.Close()

	rec := httptest.NewRecorder()
	app.uploadSiteFiles(rec, uploadRequest(t, map[string]string{
		"index.html":    "<h1>hi</h1>",
		"assets/app.js": "console.log(1)",
		"css/style.css": "body{}",
	}))
	if rec.Code != 201 {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	for _, rel := range []string{"index.html", "assets/app.js", "css/style.css"} {
		if _, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel))); err != nil {
			t.Fatalf("expected %s to exist: %v", rel, err)
		}
	}
}

func TestUploadSkipsExistingWhenOverwriteOff(t *testing.T) {
	app, db, root := newStaticFilesApp(t, "example.com")
	defer db.Close()
	if err := os.WriteFile(filepath.Join(root, "index.html"), []byte("OLD"), 0o644); err != nil {
		t.Fatal(err)
	}

	req := uploadRequest(t, map[string]string{"index.html": "NEW"})
	req.URL.RawQuery = "overwrite=0"
	rec := httptest.NewRecorder()
	app.uploadSiteFiles(rec, req)
	if rec.Code != 201 {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	body, _ := os.ReadFile(filepath.Join(root, "index.html"))
	if string(body) != "OLD" {
		t.Fatalf("overwrite=0 must keep the old file, got %q", body)
	}
	var result map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result["skipped"] != float64(1) {
		t.Fatalf("expected skipped=1, got %v", result["skipped"])
	}
}

func TestUploadRejectsTraversalInPaths(t *testing.T) {
	app, db, _ := newStaticFilesApp(t, "example.com")
	defer db.Close()

	rec := httptest.NewRecorder()
	app.uploadSiteFiles(rec, uploadRequest(t, map[string]string{"../evil.txt": "x"}))
	if rec.Code != 400 {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
}

func TestSiteFileStats(t *testing.T) {
	app, db, root := newStaticFilesApp(t, "example.com")
	defer db.Close()

	stat := func() map[string]any {
		req := httptest.NewRequest("GET", "/api/v1/proxy-hosts/1/files/stat", nil)
		req.SetPathValue("id", "1")
		rec := httptest.NewRecorder()
		app.siteFileStats(rec, req)
		if rec.Code != 200 {
			t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
		}
		var out map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}

	// An empty tree reports zero files and empty=true.
	if s := stat(); s["files"] != float64(0) || s["empty"] != true {
		t.Fatalf("expected empty, got %v", s)
	}

	// Add files, including one in a subdirectory.
	if err := os.WriteFile(filepath.Join(root, "index.html"), []byte("12345"), 0o644); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(root, "assets")
	if err := os.MkdirAll(sub, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "app.js"), []byte("abcdefghij"), 0o644); err != nil {
		t.Fatal(err)
	}

	s := stat()
	if s["files"] != float64(2) {
		t.Fatalf("expected files=2, got %v", s["files"])
	}
	if s["total_size"] != float64(15) {
		t.Fatalf("expected total_size=15, got %v", s["total_size"])
	}
	if s["empty"] != false {
		t.Fatalf("expected empty=false, got %v", s["empty"])
	}
}

func TestExtractZipRejectsBombAndMkdirBadJSONReturns400(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("big.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("0123456789ABCDEF")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	dest := t.TempDir()
	if _, err := extractZip(bytes.NewReader(buf.Bytes()), dest, 10, 5); err == nil {
		t.Fatal("extractZip must fail when decompressed bytes exceed maxBytes")
	}
	if _, statErr := os.Stat(filepath.Join(dest, "big.txt")); !os.IsNotExist(statErr) {
		t.Fatal("oversized extracted entry must be removed")
	}

	app, db, _ := newStaticFilesApp(t, "static.test")
	defer db.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/proxy-hosts/1/files/mkdir", strings.NewReader("{bad json"))
	req.SetPathValue("id", "1")
	rec := httptest.NewRecorder()
	app.mkdirSiteFile(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid JSON in mkdirSiteFile, got %d", rec.Code)
	}
}

func TestBatchSiteFileStats(t *testing.T) {
	a, db, root := newStaticFilesApp(t, "batch.test")
	if err := os.WriteFile(filepath.Join(root, "index.html"), []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO proxy_hosts(id,domain,type,root_path) VALUES(2,'proxy.test','proxy','')`); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", "/api/v1/proxy-hosts/files/stats", nil)
	rec := httptest.NewRecorder()
	a.batchSiteFileStats(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var out map[string]struct {
		Files int64 `json:"files"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if _, ok := out["2"]; ok {
		t.Fatal("proxy host must not appear in the file stats")
	}
	if got := out["1"].Files; got != 1 {
		t.Fatalf("static site file count = %d, want 1", got)
	}
}
