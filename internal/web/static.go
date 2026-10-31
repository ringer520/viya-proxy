package web

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed dist/* dist/assets/*
var files embed.FS

func Handler() http.Handler {
	root, err := fs.Sub(files, "dist")
	if err != nil {
		panic(err)
	}
	fileServer := http.FileServer(http.FS(root))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/")
		if name == "" {
			name = "index.html"
		}
		if _, err := fs.Stat(root, name); err == nil {
			fileServer.ServeHTTP(w, r)
			return
		}
		r.URL.Path = "/"
		fileServer.ServeHTTP(w, r)
	})
}
