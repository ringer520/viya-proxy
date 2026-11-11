package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequestIDMiddlewarePropagatesID(t *testing.T) {
	var seen string
	handler := requestIDMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = requestIDFrom(r.Context())
		attrs := requestLogAttrs(r)
		if len(attrs) != 2 || attrs[1] != seen {
			t.Errorf("handler log attrs lost the request id: %v", attrs)
		}
		w.WriteHeader(http.StatusCreated)
	}))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest("POST", "/api/v1/proxy-hosts", nil))
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", rec.Code)
	}
	id := rec.Header().Get("X-Request-Id")
	if len(id) != 16 {
		t.Fatalf("request id %q is not a 16-character hex token", id)
	}
	if id != seen {
		t.Fatalf("response header %q does not match the handler context %q", id, seen)
	}
}

func TestRequestIDFromEmptyContext(t *testing.T) {
	if got := requestIDFrom(httptest.NewRequest("GET", "/", nil).Context()); got != "" {
		t.Fatalf("empty context returned %q", got)
	}
}
