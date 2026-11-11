package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"time"
)

type requestIDKey struct{}

func requestIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

func newRequestID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return ""
	}
	return hex.EncodeToString(b[:])
}

// requestLogAttrs lets handlers tag their own structured logs with the same
// ID carried by the access log, the response header and the config audit row.
func requestLogAttrs(r *http.Request) []any {
	return []any{"request_id", requestIDFrom(r.Context())}
}

// requestIDMiddleware replaces the old access-log middleware. One ID ties a
// browser request to its audit row and to every log line emitted while it ran.
func requestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := newRequestID()
		if id != "" {
			w.Header().Set("X-Request-Id", id)
			r = r.WithContext(context.WithValue(r.Context(), requestIDKey{}, id))
		}
		rec := &statusRecorder{ResponseWriter: w}
		start := time.Now()
		next.ServeHTTP(rec, r)
		slog.Debug("request", "request_id", id, "method", r.Method, "path", r.URL.Path,
			"status", rec.status, "duration", time.Since(start))
		if rec.status >= 400 {
			slog.Warn("request rejected", "request_id", id, "method", r.Method,
				"path", r.URL.Path, "status", rec.status)
		}
	})
}
