package web

import (
	"bytes"
	"compress/gzip"
	"io/fs"
	"net/http"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"
)

// compressibleTypes 列出值得 gzip 的静态资源类型，同时固定 Content-Type，
// 避免依赖操作系统 MIME 注册表导致各平台响应不一致。
// 已压缩格式（webp、png、woff2）不在其中，二次压缩只会浪费 CPU。
var compressibleTypes = map[string]string{
	".css":  "text/css; charset=utf-8",
	".html": "text/html; charset=utf-8",
	".js":   "text/javascript; charset=utf-8",
	".json": "application/json; charset=utf-8",
	".map":  "application/json; charset=utf-8",
	".svg":  "image/svg+xml",
	".txt":  "text/plain; charset=utf-8",
}

// gzipMinSize 以下的小文件收益低于协商成本，直接原样返回。
const gzipMinSize = 1024

// gzipCache 缓存压缩结果。内嵌文件在进程生命周期内不会变化，
// 因此首次压缩后可永久复用，避免重复消耗 CPU。
type gzipCache struct {
	mu      sync.RWMutex
	entries map[string][]byte
}

func newGzipCache() *gzipCache {
	return &gzipCache{entries: make(map[string][]byte)}
}

func (c *gzipCache) get(name string) []byte {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.entries[name]
}

func (c *gzipCache) put(name string, data []byte) {
	c.mu.Lock()
	c.entries[name] = data
	c.mu.Unlock()
}

// gzipBytes 以最高压缩级别压缩内容（只发生一次，之后走缓存）。
func gzipBytes(src []byte) ([]byte, error) {
	var buf bytes.Buffer
	zw, err := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	if err != nil {
		return nil, err
	}
	if _, err := zw.Write(src); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// acceptsGzip 解析 Accept-Encoding，正确处理 gzip;q=0 这类显式拒绝。
func acceptsGzip(header string) bool {
	for _, part := range strings.Split(header, ",") {
		part = strings.TrimSpace(part)
		if !strings.HasPrefix(part, "gzip") {
			continue
		}
		if i := strings.Index(part, "q="); i >= 0 {
			q, err := strconv.ParseFloat(strings.TrimSpace(part[i+2:]), 64)
			if err == nil && q <= 0 {
				return false
			}
		}
		return true
	}
	return false
}

// setCacheControl 为带内容哈希的构建产物设置长缓存，
// 其余无哈希的资源（index.html、theme-init.js、图标、主题图）每次回源校验。
func setCacheControl(w http.ResponseWriter, name string) {
	if strings.HasPrefix(name, "assets/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		return
	}
	w.Header().Set("Cache-Control", "no-cache")
}

// serveCompressed 尝试以 gzip 返回静态文件；返回 false 表示交给默认文件服务。
func serveCompressed(w http.ResponseWriter, r *http.Request, root fs.FS, cache *gzipCache, name string) bool {
	contentType, ok := compressibleTypes[strings.ToLower(path.Ext(name))]
	if !ok {
		return false
	}
	// Range 请求针对的是原始字节，压缩后语义会错位，直接回退。
	if r.Header.Get("Range") != "" || !acceptsGzip(r.Header.Get("Accept-Encoding")) {
		return false
	}
	info, err := fs.Stat(root, name)
	if err != nil || info.IsDir() || info.Size() < gzipMinSize {
		return false
	}

	w.Header().Add("Vary", "Accept-Encoding")

	body := cache.get(name)
	if body == nil {
		src, readErr := fs.ReadFile(root, name)
		if readErr != nil {
			return false
		}
		body, err = gzipBytes(src)
		if err != nil {
			return false
		}
		cache.put(name, body)
	}

	w.Header().Set("Content-Encoding", "gzip")
	w.Header().Set("Content-Type", contentType)
	// The validator was computed for the identity bytes; mark the variant so a
	// cache never mixes the two representations.
	if etag := w.Header().Get("ETag"); etag != "" {
		w.Header().Set("ETag", gzipETag(etag))
	}
	http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(body))
	return true
}
