package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestSiteTextSaveAndConflict(t *testing.T) {
	a, db, root := newStaticFilesApp(t, "edit.example")
	defer db.Close()
	file := filepath.Join(root, "app.conf")
	if err := os.WriteFile(file, []byte("enabled=true\n"), 0600); err != nil {
		t.Fatal(err)
	}
	request := func(method string, body []byte) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "/?path=app.conf", bytes.NewReader(body))
		r.SetPathValue("id", "1")
		r = r.WithContext(context.WithValue(r.Context(), sessionContextKey{}, sessionContext{role: roleAdmin}))
		w := httptest.NewRecorder()
		a.siteFileText(w, r)
		return w
	}
	w := request("GET", nil)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var doc struct {
		Content, Version string
		Editable         bool
	}
	if err := json.Unmarshal(w.Body.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	if !doc.Editable || doc.Content != "enabled=true\n" {
		t.Fatal("editor metadata", doc)
	}
	body, _ := json.Marshal(map[string]string{"content": "enabled=false\n", "version": doc.Version})
	if w := request("PUT", body); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := request("PUT", body); w.Code != 409 {
		t.Fatal("stale save should conflict", w.Code)
	}
	b, _ := os.ReadFile(file)
	if string(b) != "enabled=false\n" {
		t.Fatal("unexpected content", string(b))
	}
}

func TestSiteTextRejectsBinaryAndEscape(t *testing.T) {
	rootPath := t.TempDir()
	if err := os.WriteFile(filepath.Join(rootPath, "binary"), []byte{0, 255}, 0600); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if _, _, err := readSiteText(root, "binary"); err == nil {
		t.Fatal("accepted binary data")
	}
	if _, _, err := readSiteText(root, "../outside"); err == nil {
		t.Fatal("accepted path escaping root")
	}
}
