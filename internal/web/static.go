// Package web 内嵌前端构建产物，并对外提供 SPA 静态文件服务。
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
	cache := newGzipCache()

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/")
		if name == "" {
			name = "index.html"
		}
		if _, err := fs.Stat(root, name); err != nil {
			// SPA 兜底：未知路径一律回到 index.html
			name = "index.html"
			r = r.Clone(r.Context())
			r.URL.Path = "/"
		}
		setCacheControl(w, name)
		if serveCompressed(w, r, root, cache, name) {
			return
		}
		fileServer.ServeHTTP(w, r)
	})
}
