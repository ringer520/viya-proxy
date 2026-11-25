package main

import (
	"bytes"
	"net/http"
	"sync"
	"time"
)

// Aggregated traffic is read by the panel every few seconds from several
// widgets at once. The underlying queries scan traffic_hours, so a short cache
// removes duplicate work without making the numbers visibly stale: the UI's
// own refresh interval is at least 5 seconds.
var trafficCacheTTL = 3 * time.Second

type trafficCacheEntry struct {
	body   []byte
	header http.Header
	status int
	at     time.Time
}

var trafficCache sync.Map // key -> trafficCacheEntry

// captureResponse lets the existing handlers keep using writeJSON/fail while
// the wrapper buffers one response for reuse. Traffic handlers never stream.
type captureResponse struct {
	header http.Header
	body   bytes.Buffer
	status int
}

func (c *captureResponse) Header() http.Header { return c.header }
func (c *captureResponse) Write(p []byte) (int, error) {
	if c.status == 0 {
		c.status = http.StatusOK
	}
	return c.body.Write(p)
}
func (c *captureResponse) WriteHeader(status int) {
	if c.status == 0 {
		c.status = status
	}
}

func cachedTraffic(w http.ResponseWriter, r *http.Request, name string, produce func(http.ResponseWriter)) {
	key := name + "|" + r.URL.RawQuery
	if value, ok := trafficCache.Load(key); ok {
		entry := value.(trafficCacheEntry)
		if time.Since(entry.at) < trafficCacheTTL {
			for name, values := range entry.header {
				for _, value := range values {
					w.Header().Add(name, value)
				}
			}
			w.WriteHeader(entry.status)
			_, _ = w.Write(entry.body)
			return
		}
		trafficCache.Delete(key)
	}

	capture := &captureResponse{header: make(http.Header)}
	produce(capture)
	if capture.status == http.StatusOK {
		trafficCache.Store(key, trafficCacheEntry{
			body:   append([]byte(nil), capture.body.Bytes()...),
			header: capture.header.Clone(),
			status: capture.status,
			at:     time.Now(),
		})
	}
	for name, values := range capture.header {
		for _, value := range values {
			w.Header().Add(name, value)
		}
	}
	status := capture.status
	if status == 0 {
		status = http.StatusOK
	}
	w.WriteHeader(status)
	_, _ = w.Write(capture.body.Bytes())
}
