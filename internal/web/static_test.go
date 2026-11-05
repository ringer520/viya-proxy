package web

import (
	"bytes"
	"compress/gzip"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAcceptsGzip(t *testing.T) {
	cases := map[string]bool{
		"":                            false,
		"gzip":                        true,
		"gzip, deflate, br":           true,
		"deflate":                     false,
		"gzip;q=0":                    false,
		"gzip;q=0.0":                  false,
		"gzip;q=0.5":                  true,
		"br, gzip;q=0.8":              true,
		"identity":                    false,
		"deflate, gzip;q=0, identity": false,
	}
	for header, want := range cases {
		if got := acceptsGzip(header); got != want {
			t.Errorf("acceptsGzip(%q) = %v, want %v", header, got, want)
		}
	}
}

// findAsset 返回内嵌资源中第一个指定扩展名的文件，找不到则跳过测试。
func findAsset(t *testing.T, ext string) string {
	t.Helper()
	root, err := fs.Sub(files, "dist")
	if err != nil {
		t.Fatalf("sub: %v", err)
	}
	var found string
	_ = fs.WalkDir(root, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || found != "" {
			return nil
		}
		if strings.HasSuffix(p, ext) {
			found = p
		}
		return nil
	})
	if found == "" {
		t.Skipf("内嵌资源中没有 %s 文件", ext)
	}
	return found
}

func TestHandlerServesGzipWhenAccepted(t *testing.T) {
	name := findAsset(t, ".js")
	h := Handler()

	req := httptest.NewRequest(http.MethodGet, "/"+name, nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip", got)
	}
	if !strings.Contains(rec.Header().Get("Vary"), "Accept-Encoding") {
		t.Errorf("缺少 Vary: Accept-Encoding")
	}
	if !strings.HasPrefix(rec.Header().Get("Cache-Control"), "public") {
		t.Errorf("assets 应长缓存，实际 %q", rec.Header().Get("Cache-Control"))
	}

	zr, err := gzip.NewReader(bytes.NewReader(rec.Body.Bytes()))
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	got, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("decompress: %v", err)
	}

	root, _ := fs.Sub(files, "dist")
	want, err := fs.ReadFile(root, name)
	if err != nil {
		t.Fatalf("read embedded: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("解压内容与原始文件不一致")
	}
	if len(rec.Body.Bytes()) >= len(want) {
		t.Errorf("压缩后体积未下降：%d >= %d", len(rec.Body.Bytes()), len(want))
	}
}

func TestHandlerNoGzipWithoutAcceptEncoding(t *testing.T) {
	name := findAsset(t, ".js")
	h := Handler()

	req := httptest.NewRequest(http.MethodGet, "/"+name, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("未协商 gzip 时不应压缩，Content-Encoding = %q", got)
	}
}

func TestHandlerDoesNotCompressImages(t *testing.T) {
	name := findAsset(t, ".webp")
	h := Handler()

	req := httptest.NewRequest(http.MethodGet, "/"+name, nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("已压缩格式不应再次 gzip，Content-Encoding = %q", got)
	}
}

func TestHandlerSPAFallbackServesIndex(t *testing.T) {
	h := Handler()

	req := httptest.NewRequest(http.MethodGet, "/no-such-page", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "<div id=\"app\">") {
		t.Errorf("未知路径应回退到 index.html，实际内容: %.80s", body)
	}
	if !strings.HasPrefix(rec.Header().Get("Cache-Control"), "no-cache") {
		t.Errorf("index.html 应 no-cache，实际 %q", rec.Header().Get("Cache-Control"))
	}
}
