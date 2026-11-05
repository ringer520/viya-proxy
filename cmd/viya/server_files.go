package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode/utf8"
)

// Server-wide file management. The panel operator can browse and manage the
// whole server filesystem from the "文件服务" page; the root is configurable
// (VIYA_FILES_ROOT, default "/") so a deployment can limit the exposed tree.
//
// Safety model (reusing the static-site primitives):
//   - every relative path is resolved through resolveServerPath, which refuses
//     ".." segments and guarantees the result stays inside the root;
//   - symlinks are refused for every mutation so a planted link cannot widen
//     the writable area;
//   - zip uploads refuse absolute paths, ".." segments and symlink entries, and
//     are capped by entry count and total uncompressed size.
// All endpoints are behind requireAuth, so only an authenticated admin reaches
// them. This is intentionally powerful: it is a server file manager, not a
// guest-facing share.
//
// The WebDAV share is configured separately through /api/v1/webdav-settings
// and is just one files-type proxy host.

const serverTextLimit = 1 << 20

var serverEditorMu sync.Mutex

// serverRoot returns the cleaned, absolute filesystem root the manager exposes.
func (a *App) serverRoot() string {
	return filepath.Clean(a.filesRoot)
}

// filesWriteGuard enforces read-only mode (the default, VIYA_FILES_READONLY=1).
// The systemd unit runs under ProtectSystem=strict with only /var/lib/viya and
// /opt/viya/bin writable, so the panel is a browsing tool; mutations are refused
// up front instead of surfacing confusing EROFS/EACCES errors.
func (a *App) filesWriteGuard(w http.ResponseWriter) bool {
	if a.filesReadOnly {
		fail(w, 403, errors.New("文件服务为只读模式，禁止修改"))
		return false
	}
	return true
}

// fileFailure maps filesystem errors to actionable responses instead of
// collapsing "permission denied" and "read-only filesystem" into a generic 500.
func fileFailure(w http.ResponseWriter, err error, action string) {
	switch {
	case errors.Is(err, fs.ErrPermission):
		fail(w, 403, errors.New("没有访问权限（服务账号无权"+action+"）"))
	case errors.Is(err, fs.ErrNotExist):
		fail(w, 404, errors.New("路径不存在"))
	case strings.Contains(strings.ToLower(err.Error()), "read-only"):
		fail(w, 403, errors.New("该路径为只读文件系统，无法"+action))
	default:
		fail(w, 500, errors.New("无法"+action))
	}
}

// resolveServerPath joins a caller-supplied relative path onto the root and
// guarantees the result cannot escape it. ".." is refused outright.
func resolveServerPath(root, rel string) (string, error) {
	rel = strings.TrimSpace(rel)
	if rel == "" {
		return root, nil
	}
	if strings.ContainsRune(rel, 0) {
		return "", errors.New("无效的路径")
	}
	for _, segment := range strings.FieldsFunc(rel, func(r rune) bool { return r == '/' || r == '\\' }) {
		if segment == ".." {
			return "", errors.New("路径超出了根目录范围")
		}
	}
	cleaned := filepath.Clean("/" + filepath.ToSlash(rel)) // forces a rooted, ..-collapsed form
	cleaned = strings.TrimPrefix(cleaned, "/")
	if cleaned == "" || cleaned == "." {
		return root, nil
	}
	full := filepath.Join(root, filepath.FromSlash(cleaned))
	// root may be "/", so the prefix must not double up the separator.
	prefix := root
	if root != "/" && !strings.HasSuffix(root, string(os.PathSeparator)) {
		prefix += string(os.PathSeparator)
	}
	if full != root && !strings.HasPrefix(full, prefix) {
		return "", errors.New("路径超出了根目录范围")
	}
	return full, nil
}

// serverFileEntry mirrors siteFileEntry for the server tree.
type serverFileEntry = siteFileEntry

// listServerFiles returns the contents of a directory inside the root.
func (a *App) listServerFiles(w http.ResponseWriter, r *http.Request) {
	root := a.serverRoot()
	dir := root
	if rel := r.URL.Query().Get("path"); rel != "" {
		var err error
		if dir, err = resolveServerPath(root, rel); err != nil {
			fail(w, 400, err)
			return
		}
	}
	info, err := os.Stat(dir)
	if err != nil {
		fileFailure(w, err, "读取目录")
		return
	}
	if !info.IsDir() {
		fail(w, 404, errors.New("目录不存在"))
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		fileFailure(w, err, "读取目录")
		return
	}
	items := []serverFileEntry{}
	for _, entry := range entries {
		if len(items) >= siteListMaxEntries {
			break
		}
		item := serverFileEntry{Name: entry.Name(), IsDir: entry.IsDir()}
		if entry.IsDir() {
			item.Path = filepath.ToSlash(entry.Name())
		} else {
			if fileInfo, statErr := entry.Info(); statErr == nil {
				item.Size = fileInfo.Size()
				item.Modified = fileInfo.ModTime().UTC().Format("2006-01-02T15:04:05Z")
			}
			item.Path = filepath.ToSlash(entry.Name())
		}
		items = append(items, item)
	}
	parent := ""
	if dir != root {
		if rel, err := filepath.Rel(root, filepath.Dir(dir)); err == nil && rel != "." {
			parent = filepath.ToSlash(rel)
		}
	}
	writeJSON(w, 200, map[string]any{
		"path":      filepath.ToSlash(strings.TrimPrefix(dir, root)),
		"parent":    parent,
		"items":     items,
		"truncated": len(entries) > siteListMaxEntries,
		"writable":  !a.filesReadOnly,
	})
}

// serverFileTree returns the immediate sub-directories of a directory so the
// left-hand tree can expand lazily. has_children tells the UI whether a node is
// worth expanding.
func (a *App) serverFileTree(w http.ResponseWriter, r *http.Request) {
	root := a.serverRoot()
	dir := root
	if rel := r.URL.Query().Get("path"); rel != "" {
		var err error
		if dir, err = resolveServerPath(root, rel); err != nil {
			fail(w, 400, err)
			return
		}
	}
	info, err := os.Stat(dir)
	if err != nil {
		fileFailure(w, err, "读取目录")
		return
	}
	if !info.IsDir() {
		fail(w, 404, errors.New("目录不存在"))
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		fileFailure(w, err, "读取目录")
		return
	}
	type treeNode struct {
		Name        string `json:"name"`
		Path        string `json:"path"`
		HasChildren bool   `json:"has_children"`
		Readable    bool   `json:"readable"`
	}
	nodes := []treeNode{}
	for _, entry := range entries {
		if len(nodes) >= siteListMaxEntries {
			break
		}
		if !entry.IsDir() {
			continue
		}
		rel := filepath.ToSlash(entry.Name())
		has, readable := false, true
		if sub, sErr := os.ReadDir(filepath.Join(dir, entry.Name())); sErr != nil {
			readable = false
		} else {
			for _, s := range sub {
				if s.IsDir() {
					has = true
					break
				}
			}
		}
		nodes = append(nodes, treeNode{Name: entry.Name(), Path: rel, HasChildren: has, Readable: readable})
	}
	writeJSON(w, 200, map[string]any{
		"path": filepath.ToSlash(strings.TrimPrefix(dir, root)),
		"dirs": nodes,
	})
}

// mkdirServerFile creates a folder below the root.
func (a *App) mkdirServerFile(w http.ResponseWriter, r *http.Request) {
	if !a.filesWriteGuard(w) {
		return
	}
	root := a.serverRoot()
	var input struct {
		Path string `json:"path"`
	}
	if err := decodeJSONBody(w, r, &input); err != nil {
		return
	}
	full, err := resolveServerPath(root, input.Path)
	if err != nil {
		fail(w, 400, err)
		return
	}
	if full == root {
		fail(w, 400, errors.New("无效的路径"))
		return
	}
	if err := refuseSymlink(root, full); err != nil {
		fail(w, 400, err)
		return
	}
	if err := os.MkdirAll(full, siteMkdirMode); err != nil {
		fail(w, 500, errors.New("无法创建目录"))
		return
	}
	writeJSON(w, 201, map[string]any{"path": filepath.ToSlash(strings.TrimPrefix(full, root))})
}

// touchServerFile creates an empty file below the root (新建文件).
func (a *App) touchServerFile(w http.ResponseWriter, r *http.Request) {
	if !a.filesWriteGuard(w) {
		return
	}
	root := a.serverRoot()
	var input struct {
		Path string `json:"path"`
	}
	if err := decodeJSONBody(w, r, &input); err != nil {
		return
	}
	full, err := resolveServerPath(root, input.Path)
	if err != nil {
		fail(w, 400, err)
		return
	}
	if full == root {
		fail(w, 400, errors.New("无效的路径"))
		return
	}
	if err := refuseSymlink(root, full); err != nil {
		fail(w, 400, err)
		return
	}
	if err := os.MkdirAll(filepath.Dir(full), siteMkdirMode); err != nil {
		fail(w, 500, errors.New("无法创建目录"))
		return
	}
	f, err := os.OpenFile(full, os.O_CREATE|os.O_EXCL|os.O_WRONLY, siteFileMode)
	if err != nil {
		if os.IsExist(err) {
			fail(w, 409, errors.New("文件已存在"))
			return
		}
		fail(w, 500, errors.New("无法创建文件"))
		return
	}
	f.Close()
	writeJSON(w, 201, map[string]any{"path": filepath.ToSlash(strings.TrimPrefix(full, root))})
}

// uploadServerFiles stores uploaded files below the root. A file's relative
// structure is preserved via the parallel "paths" field (see uploadSiteFiles).
func (a *App) uploadServerFiles(w http.ResponseWriter, r *http.Request) {
	if !a.filesWriteGuard(w) {
		return
	}
	root := a.serverRoot()
	target := root
	if rel := r.URL.Query().Get("path"); rel != "" {
		var err error
		if target, err = resolveServerPath(root, rel); err != nil {
			fail(w, 400, err)
			return
		}
	}
	if err := refuseSymlink(root, target); err != nil {
		fail(w, 400, err)
		return
	}
	if err := os.MkdirAll(target, siteMkdirMode); err != nil {
		fail(w, 500, errors.New("无法创建目标目录"))
		return
	}
	overwrite := r.URL.Query().Get("overwrite") != "0"
	r.Body = http.MaxBytesReader(w, r.Body, siteFileMaxRequest)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		fail(w, 400, errors.New("无效的上传内容（是否过大？）"))
		return
	}
	form := r.MultipartForm
	if form == nil || len(form.File["files"]) == 0 {
		fail(w, 400, errors.New("没有上传任何文件"))
		return
	}
	stored, extracted, skipped := 0, 0, 0
	files := form.File["files"]
	paths := form.Value["paths"]
	for i, header := range files {
		rel := filepath.Base(filepath.FromSlash(header.Filename))
		if i < len(paths) {
			if p := strings.TrimSpace(filepath.FromSlash(paths[i])); p != "" {
				rel = p
			}
		}
		if rel == "" || rel == "." {
			fail(w, 400, errors.New("无效的文件名"))
			return
		}
		dstPath, err := resolveServerPath(target, rel)
		if err != nil {
			fail(w, 400, err)
			return
		}
		if err := refuseSymlink(root, dstPath); err != nil {
			fail(w, 400, err)
			return
		}
		src, err := header.Open()
		if err != nil {
			fail(w, 500, errors.New("无法读取上传的文件"))
			return
		}
		if strings.EqualFold(filepath.Ext(filepath.Base(rel)), ".zip") {
			count, err := extractZip(src, target, siteZipMaxEntries, siteZipMaxUnpacked)
			src.Close()
			if err != nil {
				fail(w, 400, fmt.Errorf("压缩包 %s：%w", filepath.Base(rel), err))
				return
			}
			extracted += count
			continue
		}
		if !overwrite {
			if _, statErr := os.Lstat(dstPath); statErr == nil {
				src.Close()
				skipped++
				continue
			}
		}
		if err := os.MkdirAll(filepath.Dir(dstPath), siteMkdirMode); err != nil {
			src.Close()
			fail(w, 500, errors.New("无法创建目录"))
			return
		}
		dst, err := os.OpenFile(dstPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, siteFileMode)
		if err != nil {
			src.Close()
			fail(w, 500, errors.New("无法写入文件"))
			return
		}
		_, copyErr := io.Copy(dst, src)
		src.Close()
		closeErr := dst.Close()
		if copyErr != nil || closeErr != nil {
			_ = os.Remove(dstPath)
			fail(w, 500, errors.New("无法保存文件"))
			return
		}
		stored++
	}
	writeJSON(w, 201, map[string]any{"stored": stored, "extracted": extracted, "skipped": skipped})
}

// downloadServerFile streams one file out of the root.
func (a *App) downloadServerFile(w http.ResponseWriter, r *http.Request) {
	root := a.serverRoot()
	full, err := resolveServerPath(root, r.URL.Query().Get("path"))
	if err != nil {
		fail(w, 400, err)
		return
	}
	if err := refuseSymlink(root, full); err != nil {
		fail(w, 400, err)
		return
	}
	info, err := os.Stat(full)
	if err != nil {
		fileFailure(w, err, "读取文件")
		return
	}
	if info.IsDir() {
		fail(w, 404, errors.New("文件不存在"))
		return
	}
	w.Header().Set("Content-Disposition", `attachment; filename="`+filepath.Base(full)+`"`)
	http.ServeFile(w, r, full)
}

// renameServerFile moves a file or directory to a new base name inside the same
// parent.
func (a *App) renameServerFile(w http.ResponseWriter, r *http.Request) {
	if !a.filesWriteGuard(w) {
		return
	}
	root := a.serverRoot()
	var input struct {
		Path string `json:"path"`
		Name string `json:"name"`
	}
	if err := decodeJSONBody(w, r, &input); err != nil {
		return
	}
	full, err := resolveServerPath(root, input.Path)
	if err != nil || full == root {
		fail(w, 400, errors.New("无效的路径"))
		return
	}
	name := strings.TrimSpace(input.Name)
	if name == "" || strings.ContainsAny(name, "/\\") || name == "." || name == ".." {
		fail(w, 400, errors.New("无效的新名称"))
		return
	}
	newFull := filepath.Join(filepath.Dir(full), name)
	if err := refuseSymlink(root, full); err != nil {
		fail(w, 400, err)
		return
	}
	if _, statErr := os.Lstat(newFull); statErr == nil {
		fail(w, 409, errors.New("目标名称已存在"))
		return
	}
	if err := os.Rename(full, newFull); err != nil {
		fail(w, 500, errors.New("重命名失败"))
		return
	}
	writeJSON(w, 200, map[string]any{"path": filepath.ToSlash(strings.TrimPrefix(newFull, root))})
}

// deleteServerFile removes a file, or a directory when recursive is requested.
func (a *App) deleteServerFile(w http.ResponseWriter, r *http.Request) {
	if !a.filesWriteGuard(w) {
		return
	}
	root := a.serverRoot()
	full, err := resolveServerPath(root, r.URL.Query().Get("path"))
	if err != nil || full == root {
		fail(w, 400, errors.New("无效或受保护的路径"))
		return
	}
	if err := refuseSymlink(root, full); err != nil {
		fail(w, 400, err)
		return
	}
	info, err := os.Lstat(full)
	if err != nil {
		fail(w, 404, errors.New("未找到"))
		return
	}
	if !info.IsDir() {
		if err := os.Remove(full); err != nil {
			fail(w, 500, errors.New("无法删除文件"))
			return
		}
		writeJSON(w, 200, map[string]any{"deleted": 1})
		return
	}
	if r.URL.Query().Get("recursive") != "1" {
		entries, err := os.ReadDir(full)
		if err != nil {
			fail(w, 500, errors.New("无法读取目录"))
			return
		}
		if len(entries) > 0 {
			fail(w, 400, errors.New("目录不为空，请确认递归删除"))
			return
		}
		if err := os.Remove(full); err != nil {
			fail(w, 500, errors.New("无法删除目录"))
			return
		}
		writeJSON(w, 200, map[string]any{"deleted": 1})
		return
	}
	deleted, err := removeTree(full, siteDeleteMaxFiles)
	if err != nil {
		fail(w, 400, err)
		return
	}
	writeJSON(w, 200, map[string]any{"deleted": deleted})
}

// serverTextFile opens the root for a text operation, returning a confined root
// handle plus the relative name.
func (a *App) serverTextFile(r *http.Request) (*os.Root, string, error) {
	root := a.serverRoot()
	full, err := resolveServerPath(root, r.URL.Query().Get("path"))
	if err != nil || full == root {
		return nil, "", errors.New("请选择根目录内的文件")
	}
	if err := refuseSymlink(root, full); err != nil {
		return nil, "", err
	}
	rel, err := filepath.Rel(root, full)
	if err != nil {
		return nil, "", err
	}
	fs, err := os.OpenRoot(root)
	if err != nil {
		return nil, "", errors.New("无法打开根目录，请检查服务权限")
	}
	return fs, rel, err
}

// serverFileText reads or writes a UTF-8 text file below the root.
func (a *App) serverFileText(w http.ResponseWriter, r *http.Request) {
	fs, name, err := a.serverTextFile(r)
	if err != nil {
		fail(w, 400, err)
		return
	}
	defer fs.Close()
	w.Header().Set("Cache-Control", "no-store")
	serverEditorMu.Lock()
	defer serverEditorMu.Unlock()
	initial, err := fs.Lstat(name)
	if err != nil || !initial.Mode().IsRegular() {
		fail(w, 400, errors.New("仅支持普通文本文件，不能编辑符号链接"))
		return
	}
	if r.Method == http.MethodGet {
		b, info, err := readSiteText(fs, name)
		if err != nil {
			fail(w, 400, err)
			return
		}
		_ = info
		version := fmt.Sprintf("%x", sha256.Sum256(b))
		writeJSON(w, 200, map[string]any{"content": string(b), "version": version, "editable": true})
		return
	}
	if !a.filesWriteGuard(w) {
		return
	}
	var body struct {
		Content string `json:"content"`
		Version string `json:"version"`
	}
	defer r.Body.Close()
	r.Body = http.MaxBytesReader(w, r.Body, serverTextLimit*6+4096)
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		fail(w, 400, err)
		return
	}
	b, info, err := readSiteText(fs, name)
	if err != nil {
		fail(w, 400, err)
		return
	}
	version := fmt.Sprintf("%x", sha256.Sum256(b))
	if body.Version != version {
		fail(w, 409, errors.New("文件已被其他程序修改，请重新打开后再编辑"))
		return
	}
	if len(body.Content) > serverTextLimit || !utf8.ValidString(body.Content) || strings.ContainsRune(body.Content, 0) {
		fail(w, 400, errors.New("仅支持 1 MiB 以内的 UTF-8 文本"))
		return
	}
	var token [16]byte
	if _, err := io.ReadFull(rand.Reader, token[:]); err != nil {
		fail(w, 500, err)
		return
	}
	temp := filepath.Join(filepath.Dir(name), fmt.Sprintf(".viya-edit-%x", token))
	f, err := fs.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm())
	if err != nil {
		fail(w, 500, errors.New("无法写入文件，请检查服务的目录权限"))
		return
	}
	defer fs.Remove(temp)
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
	if err != nil {
		fail(w, 500, errors.New("保存文件失败"))
		return
	}
	if err := fs.Rename(temp, name); err != nil {
		fail(w, 500, errors.New("保存文件失败"))
		return
	}
	writeJSON(w, 200, map[string]any{"version": fmt.Sprintf("%x", sha256.Sum256([]byte(body.Content)))})
}
