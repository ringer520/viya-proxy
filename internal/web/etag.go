package web

import (
	"fmt"
	"hash/fnv"
	"io/fs"
	"net/http"
	"strings"
	"sync"
)

// etagIndex caches one validator per embedded file. The content cannot change
// while the process runs, so each file is hashed at most once.
var etagIndex sync.Map

// etagFor returns a weak validator for a file in the embedded tree.
//
// embed.FS reports a zero modification time, so http.ServeContent sets neither
// Last-Modified nor ETag. A browser holding a "no-cache" copy then has nothing
// to revalidate with and downloads the whole file again on every panel load —
// the theme artwork alone is several hundred kilobytes per visit.
func etagFor(root fs.FS, name string) string {
	if cached, ok := etagIndex.Load(name); ok {
		return cached.(string)
	}
	data, err := fs.ReadFile(root, name)
	if err != nil {
		return ""
	}
	sum := fnv.New64a()
	_, _ = sum.Write(data)
	etag := fmt.Sprintf(`W/"%x-%x"`, len(data), sum.Sum64())
	etagIndex.Store(name, etag)
	return etag
}

// gzipETag marks the compressed representation. Both variants carry the same
// bytes, so etagMatches ignores the suffix: a weak validator may cover either.
func gzipETag(etag string) string {
	return strings.TrimSuffix(strings.TrimSpace(etag), `"`) + `-gzip"`
}

// etagMatches reports whether an If-None-Match header covers etag.
func etagMatches(header, etag string) bool {
	if header == "" {
		return false
	}
	want := etagCore(etag)
	for _, candidate := range strings.Split(header, ",") {
		candidate = strings.TrimSpace(candidate)
		if candidate == "*" {
			return true
		}
		if etagCore(candidate) == want {
			return true
		}
	}
	return false
}

// etagCore strips the weakness marker, the quotes and the encoding suffix so
// identity and gzip responses compare equal.
func etagCore(etag string) string {
	value := strings.TrimSpace(etag)
	value = strings.TrimPrefix(value, "W/")
	value = strings.Trim(value, `"`)
	return strings.TrimSuffix(value, "-gzip")
}

// serveNotModified reports whether the request already holds this file and, if
// so, answers 304. It must run before any body is written.
func serveNotModified(w http.ResponseWriter, r *http.Request, etag string) bool {
	if etag == "" {
		return false
	}
	w.Header().Set("ETag", etag)
	if !etagMatches(r.Header.Get("If-None-Match"), etag) {
		return false
	}
	w.WriteHeader(http.StatusNotModified)
	return true
}
