package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode/utf8"
)

const siteTextLimit = 1 << 20

var siteEditorMu sync.Mutex

// Keep file access rooted even if a directory is replaced with a symlink.
func (a *App) siteTextFile(r *http.Request) (*os.Root, string, bool, error) {
	id, root, err := a.loadStaticSite(r)
	if err != nil {
		return nil, "", false, err
	}
	full, err := resolveWithinRoot(root, r.URL.Query().Get("path"))
	if err != nil || full == root {
		return nil, "", false, errors.New("请选择目录内的文件")
	}
	rel, err := filepath.Rel(root, full)
	if err != nil {
		return nil, "", false, err
	}
	var kind string
	if err = a.db.QueryRowContext(r.Context(), "SELECT type FROM proxy_hosts WHERE id=?", id).Scan(&kind); err != nil {
		return nil, "", false, err
	}
	fs, err := os.OpenRoot(root)
	return fs, rel, a.underManagedRoot(root) || kind == "files", err
}

func readSiteText(root *os.Root, name string) ([]byte, os.FileInfo, error) {
	initial, err := root.Lstat(name)
	if err != nil || !initial.Mode().IsRegular() {
		return nil, nil, errors.New("仅支持普通文本文件，不能编辑符号链接")
	}
	f, err := root.Open(name)
	if err != nil {
		return nil, nil, errors.New("无法打开文件")
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, nil, errors.New("仅支持普通文本文件")
	}
	if info.Size() > siteTextLimit {
		return nil, nil, errors.New("文本预览和编辑仅支持 1 MiB 以内的文件")
	}
	b, err := io.ReadAll(io.LimitReader(f, siteTextLimit+1))
	if err != nil || len(b) > siteTextLimit {
		return nil, nil, errors.New("文件读取失败或超过大小限制")
	}
	if !utf8.Valid(b) || strings.ContainsRune(string(b), 0) {
		return nil, nil, errors.New("该文件不是 UTF-8 文本，请下载后查看")
	}
	return b, info, nil
}

func (a *App) siteFileText(w http.ResponseWriter, r *http.Request) {
	root, name, writable, err := a.siteTextFile(r)
	if err != nil {
		fail(w, 400, err)
		return
	}
	defer root.Close()
	w.Header().Set("Cache-Control", "no-store")
	siteEditorMu.Lock()
	defer siteEditorMu.Unlock()
	b, info, err := readSiteText(root, name)
	if err != nil {
		fail(w, 400, err)
		return
	}
	version := fmt.Sprintf("%x", sha256.Sum256(b))
	if r.Method == http.MethodGet {
		session, _ := r.Context().Value(sessionContextKey{}).(sessionContext)
		writeJSON(w, 200, map[string]any{"content": string(b), "version": version, "editable": writable && session.role == roleAdmin})
		return
	}
	if !writable {
		fail(w, 403, errors.New("该目录只读"))
		return
	}
	var body struct {
		Content string `json:"content"`
		Version string `json:"version"`
	}
	defer r.Body.Close()
	r.Body = http.MaxBytesReader(w, r.Body, siteTextLimit*6+4096)
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		fail(w, 400, err)
		return
	}
	if body.Version != version {
		fail(w, 409, errors.New("文件已被其他程序修改，请重新打开后再编辑"))
		return
	}
	if len(body.Content) > siteTextLimit || !utf8.ValidString(body.Content) || strings.ContainsRune(body.Content, 0) {
		fail(w, 400, errors.New("仅支持 1 MiB 以内的 UTF-8 文本"))
		return
	}
	// Root confinement also applies to the temporary file and the final rename.
	var token [16]byte
	if _, err := io.ReadFull(rand.Reader, token[:]); err != nil {
		fail(w, 500, err)
		return
	}
	temp := filepath.Join(filepath.Dir(name), fmt.Sprintf(".viya-edit-%x", token))
	f, err := root.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm())
	if err != nil {
		fail(w, 500, errors.New("无法写入文件，请检查服务的目录权限"))
		return
	}
	defer root.Remove(temp)
	err = f.Chmod(info.Mode().Perm())
	if err == nil {
		_, err = f.WriteString(body.Content)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		current, _, readErr := readSiteText(root, name)
		if readErr != nil || fmt.Sprintf("%x", sha256.Sum256(current)) != version {
			fail(w, 409, errors.New("文件已被其他程序修改，请重新打开后再编辑"))
			return
		}
		err = root.Rename(temp, name)
	}
	if err != nil {
		fail(w, 500, errors.New("保存文件失败"))
		return
	}
	writeJSON(w, 200, map[string]any{"version": fmt.Sprintf("%x", sha256.Sum256([]byte(body.Content)))})
}
