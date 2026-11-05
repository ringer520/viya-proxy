package main

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Static-site file management. These endpoints exist for the convenience of
// dropping a small static site into the managed tree; they are deliberately
// narrow: no editing, no permissions, and writes only inside
// <data>/sites where the service is allowed to write.
//
// Safety model:
//   - only sites of type "static" have files;
//   - read (list/download) works for any root, mutations only inside the
//     managed tree (external roots are served read-only);
//   - every requested path is resolved and must stay inside the site root;
//   - symlinks are refused for mutations so a planted link cannot widen the
//     writable area;
//   - zip uploads refuse absolute paths, ".." segments and symlink entries,
//     and are capped by entry count and total uncompressed size.

const (
	siteFileMaxRequest = 1 << 30 // 1 GiB request cap for uploads
	siteZipMaxEntries  = 5000
	siteZipMaxUnpacked = 1 << 30 // 1 GiB total uncompressed
	siteListMaxEntries = 2000
	siteDeleteMaxFiles = 5000
	siteMkdirMode      = 0750
	siteFileMode       = 0644
	siteStatMaxFiles   = 10000 // cap so a huge tree cannot stall the request
)

// siteFileEntry describes one item inside a static site's root.
type siteFileEntry struct {
	Name     string `json:"name"`
	Path     string `json:"path"`
	IsDir    bool   `json:"is_dir"`
	Size     int64  `json:"size"`
	Modified string `json:"modified"`
}

// loadStaticSite returns the site's id and cleaned root directory. It refuses
// other site types so the file API cannot be pointed at a proxy upstream.
// File services serve a directory too, so they share this path.
func (a *App) loadStaticSite(r *http.Request) (int64, string, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		return 0, "", errors.New("站点 ID 无效")
	}
	var siteType, root string
	if err := a.db.QueryRowContext(r.Context(), `SELECT type,root_path FROM proxy_hosts WHERE id=?`, id).Scan(&siteType, &root); err != nil {
		return 0, "", errors.New("站点不存在")
	}
	if siteType != "static" && siteType != "files" {
		return 0, "", errors.New("只有静态站点与文件服务站点可以管理文件")
	}
	root = strings.TrimSpace(root)
	if err := validateStaticRoot(root); err != nil {
		return 0, "", err
	}
	return id, filepath.Clean(root), nil
}

// underManagedRoot reports whether the site root lives in the tree the panel
// may write to. External roots are served, never modified.
func (a *App) underManagedRoot(root string) bool {
	base := filepath.Clean(a.managedSitesDir())
	return root == base || strings.HasPrefix(root, base+string(os.PathSeparator))
}

// validateManagedStaticSiteRoot refuses the managed-sites root itself,
// external paths, and symlinked paths before a destructive operation starts.
func (a *App) validateManagedStaticSiteRoot(root string) error {
	root = filepath.Clean(strings.TrimSpace(root))
	base := filepath.Clean(a.managedSitesDir())
	if root == "." || root == base || !a.underManagedRoot(root) {
		return errors.New("拒绝删除管理目录之外的静态站点")
	}
	if err := refuseSymlink(base, root); err != nil {
		return err
	}
	info, err := os.Lstat(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errors.New("静态站点根目录必须是面板管理的目录")
	}
	return nil
}

// removeManagedStaticSite removes only a previously validated static site
// directory. It never follows symlinks or removes the managed-sites root.
func (a *App) removeManagedStaticSite(root string) error {
	if err := a.validateManagedStaticSiteRoot(root); err != nil {
		return err
	}
	root = filepath.Clean(strings.TrimSpace(root))
	return os.RemoveAll(root)
}

// resolveWithinRoot joins a caller-supplied relative path onto the site root
// and guarantees the result cannot escape it. Any ".." segment is refused
// outright rather than normalised away, so behaviour is identical on every
// platform.
func resolveWithinRoot(root, rel string) (string, error) {
	rel = strings.TrimSpace(rel)
	if rel == "" {
		return root, nil
	}
	if strings.ContainsRune(rel, 0) {
		return "", errors.New("路径无效")
	}
	for _, segment := range strings.FieldsFunc(rel, func(r rune) bool { return r == '/' || r == '\\' }) {
		if segment == ".." {
			return "", errors.New("路径超出站点根目录")
		}
	}
	cleaned := filepath.Clean("/" + filepath.ToSlash(rel)) // forces a rooted, ..-collapsed form
	cleaned = strings.TrimPrefix(cleaned, "/")
	if cleaned == "" || cleaned == "." {
		return root, nil
	}
	full := filepath.Join(root, filepath.FromSlash(cleaned))
	rootPrefix := root + string(os.PathSeparator)
	if full != root && !strings.HasPrefix(full, rootPrefix) {
		return "", errors.New("路径超出站点根目录")
	}
	return full, nil
}

// refuseSymlink walks every existing component below root and rejects symlinks
// so mutations cannot be redirected outside the tree.
func refuseSymlink(root, full string) error {
	rel, err := filepath.Rel(root, full)
	if err != nil {
		return errors.New("路径无效")
	}
	current := root
	if rel == "." {
		return nil
	}
	for _, segment := range strings.Split(rel, string(os.PathSeparator)) {
		current = filepath.Join(current, segment)
		info, err := os.Lstat(current)
		if err != nil {
			return nil // not created yet; creation cannot follow a link
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("此处不允许使用符号链接")
		}
	}
	return nil
}

func (a *App) siteFileLocation(r *http.Request) (root, full string, err error) {
	_, root, err = a.loadStaticSite(r)
	if err != nil {
		return "", "", err
	}
	full, err = resolveWithinRoot(root, r.URL.Query().Get("path"))
	if err != nil {
		return "", "", err
	}
	return root, full, nil
}

// listSiteFiles returns the contents of a directory inside the site.
func (a *App) listSiteFiles(w http.ResponseWriter, r *http.Request) {
	_, root, err := a.loadStaticSite(r)
	if err != nil {
		fail(w, 400, err)
		return
	}
	dir := root
	if rel := r.URL.Query().Get("path"); rel != "" {
		if dir, err = resolveWithinRoot(root, rel); err != nil {
			fail(w, 400, err)
			return
		}
	}
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		fail(w, 404, errors.New("目录不存在"))
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		fail(w, 500, errors.New("无法读取目录"))
		return
	}
	items := []siteFileEntry{}
	for _, entry := range entries {
		if len(items) >= siteListMaxEntries {
			break
		}
		item := siteFileEntry{Name: entry.Name(), IsDir: entry.IsDir()}
		if entry.IsDir() {
			item.Path = strings.TrimSuffix(strings.TrimPrefix(dir+string(os.PathSeparator)+entry.Name(), root+string(os.PathSeparator)), string(os.PathSeparator))
		} else {
			if fileInfo, statErr := entry.Info(); statErr == nil {
				item.Size = fileInfo.Size()
				item.Modified = fileInfo.ModTime().UTC().Format("2006-01-02T15:04:05Z")
			}
			item.Path, _ = filepath.Rel(root, filepath.Join(dir, entry.Name()))
		}
		item.Path = filepath.ToSlash(item.Path)
		items = append(items, item)
	}
	writeJSON(w, 200, map[string]any{
		"items":     items,
		"truncated": len(entries) > siteListMaxEntries,
		"writable":  a.underManagedRoot(root),
		"path":      relPath(root, dir),
	})
}

// siteFileStats reports how many files live under a static site's root and
// their total size, so the host card can show whether the site has content.
// Counting is capped so a huge tree cannot stall the request.
func (a *App) siteFileStats(w http.ResponseWriter, r *http.Request) {
	_, root, err := a.loadStaticSite(r)
	if err != nil {
		fail(w, 400, err)
		return
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		writeJSON(w, 200, map[string]any{"files": 0, "total_size": 0, "empty": true})
		return
	}
	var files int64
	var total int64
	truncated := false
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // unreadable subtree: skip rather than fail the whole stat
		}
		if d.IsDir() {
			return nil
		}
		if files >= siteStatMaxFiles {
			truncated = true
			return fs.SkipAll
		}
		if fi, e := d.Info(); e == nil {
			files++
			total += fi.Size()
		}
		return nil
	})
	if err != nil {
		fail(w, 500, errors.New("无法检查站点目录"))
		return
	}
	writeJSON(w, 200, map[string]any{
		"files":      files,
		"total_size": total,
		"empty":      files == 0,
		"truncated":  truncated,
	})
}

func relPath(root, full string) string {
	rel, err := filepath.Rel(root, full)
	if err != nil {
		return ""
	}
	return filepath.ToSlash(rel)
}

// downloadSiteFile streams one file out of the site (useful to verify what was
// uploaded without leaving the panel).
func (a *App) downloadSiteFile(w http.ResponseWriter, r *http.Request) {
	root, full, err := a.siteFileLocation(r)
	if err != nil {
		fail(w, 400, err)
		return
	}
	if err := refuseSymlink(root, full); err != nil {
		fail(w, 400, err)
		return
	}
	info, err := os.Stat(full)
	if err != nil || info.IsDir() {
		fail(w, 404, errors.New("文件不存在"))
		return
	}
	w.Header().Set("Content-Disposition", `attachment; filename="`+filepath.Base(full)+`"`)
	http.ServeFile(w, r, full)
}

// uploadSiteFiles stores uploaded files below the site root. A file's name may
// carry a relative directory path (a directory upload reports
// "dist/assets/app.js"); that structure is preserved under the target folder.
// A .zip payload is extracted in place instead of being stored.
//
// Query params:
//   - path: target directory inside the site (defaults to the root);
//   - overwrite: "0" skips files that already exist instead of replacing them.
func (a *App) uploadSiteFiles(w http.ResponseWriter, r *http.Request) {
	_, root, err := a.loadStaticSite(r)
	if err != nil {
		fail(w, 400, err)
		return
	}
	if !a.underManagedRoot(root) {
		fail(w, 400, errors.New("该站点目录由面板之外管理，请直接上传到那里"))
		return
	}
	target := root
	if rel := r.URL.Query().Get("path"); rel != "" {
		if target, err = resolveWithinRoot(root, rel); err != nil {
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
		fail(w, 400, errors.New("上传内容无效（可能过大）"))
		return
	}
	form := r.MultipartForm
	if form == nil || len(form.File["files"]) == 0 {
		fail(w, 400, errors.New("没有上传任何文件"))
		return
	}
	stored, extracted, skipped := 0, 0, 0
	// Relative paths travel in a parallel "paths" field because the multipart
	// layer sanitises file names to their base name (CVE-2023-24540), which
	// would flatten a directory upload.
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
			fail(w, 400, errors.New("文件名无效"))
			return
		}
		dstPath, err := resolveWithinRoot(target, rel)
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
			fail(w, 500, errors.New("无法读取上传文件"))
			return
		}
		// A zip archive unpacks into the current target (its entries carry
		// their own structure); the archive file itself is never stored.
		if strings.EqualFold(filepath.Ext(filepath.Base(rel)), ".zip") {
			count, err := extractZip(src, target, siteZipMaxEntries, siteZipMaxUnpacked)
			src.Close()
			if err != nil {
				fail(w, 400, fmt.Errorf("zip %s: %w", filepath.Base(rel), err))
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

// extractZip unpacks an archive below dest with zip-slip and bomb guards.
func extractZip(src io.Reader, dest string, maxEntries int, maxBytes int64) (int, error) {
	// Buffer into a temp file: zip needs random access and the payload may be
	// large. The reader is already capped by http.MaxBytesReader upstream.
	tmp, err := os.CreateTemp("", "viya-upload-*.zip")
	if err != nil {
		return 0, errors.New("无法暂存归档")
	}
	defer os.Remove(tmp.Name())
	if _, err := io.Copy(tmp, src); err != nil {
		tmp.Close()
		return 0, errors.New("无法读取归档")
	}
	if err := tmp.Close(); err != nil {
		return 0, errors.New("无法读取归档")
	}
	reader, err := zip.OpenReader(tmp.Name())
	if err != nil {
		return 0, errors.New("不是有效的 zip 归档")
	}
	defer reader.Close()
	if len(reader.File) > maxEntries {
		return 0, errors.New("归档条目过多")
	}
	destPrefix := dest + string(os.PathSeparator)
	written := int64(0)
	count := 0
	for _, file := range reader.File {
		name := filepath.FromSlash(file.Name)
		if filepath.IsAbs(name) {
			return count, fmt.Errorf("entry %s uses an absolute path", file.Name)
		}
		for _, segment := range strings.FieldsFunc(file.Name, func(r rune) bool { return r == '/' || r == '\\' }) {
			if segment == ".." {
				return count, fmt.Errorf("entry %s escapes the site root", file.Name)
			}
		}
		target := filepath.Join(dest, filepath.Clean("/"+filepath.ToSlash(name)))
		if target != dest && !strings.HasPrefix(target, destPrefix) {
			return count, fmt.Errorf("entry %s escapes the site root", file.Name)
		}
		if file.Mode()&os.ModeSymlink != 0 {
			return count, fmt.Errorf("entry %s is a symbolic link", file.Name)
		}
		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(target, siteMkdirMode); err != nil {
				return count, errors.New("无法创建目录")
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), siteMkdirMode); err != nil {
			return count, errors.New("无法创建目录")
		}
		written += int64(file.UncompressedSize64)
		if written > maxBytes {
			return count, errors.New("归档解压后超过大小限制")
		}
		in, err := file.Open()
		if err != nil {
			return count, errors.New("无法读取归档条目")
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, siteFileMode)
		if err != nil {
			in.Close()
			return count, errors.New("无法写入归档条目")
		}
		_, copyErr := io.Copy(out, in)
		in.Close()
		closeErr := out.Close()
		if copyErr != nil || closeErr != nil {
			_ = os.Remove(target)
			return count, errors.New("无法保存归档条目")
		}
		count++
	}
	return count, nil
}

// deleteSiteFile removes a file, or a directory when recursive is requested.
func (a *App) deleteSiteFile(w http.ResponseWriter, r *http.Request) {
	root, full, err := a.siteFileLocation(r)
	if err != nil {
		fail(w, 400, err)
		return
	}
	if !a.underManagedRoot(root) {
		fail(w, 400, errors.New("该站点目录由面板之外管理"))
		return
	}
	if err := refuseSymlink(root, full); err != nil {
		fail(w, 400, err)
		return
	}
	if full == root {
		fail(w, 400, errors.New("拒绝删除站点根目录"))
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
			fail(w, 400, errors.New("目录非空，请确认递归删除"))
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

// removeTree deletes a directory after checking it does not grow past the
// entry cap, so a delete request cannot be used to walk an unbounded tree.
func removeTree(root string, maxEntries int) (int, error) {
	count := 0
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		count++
		if count > maxEntries {
			return errors.New("目录条目过多，无法一次删除")
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	if err := os.RemoveAll(root); err != nil {
		return 0, errors.New("无法删除目录")
	}
	return count, nil
}

// mkdirSiteFile creates a folder below the site root.
func (a *App) mkdirSiteFile(w http.ResponseWriter, r *http.Request) {
	_, root, err := a.loadStaticSite(r)
	if err != nil {
		fail(w, 400, err)
		return
	}
	if !a.underManagedRoot(root) {
		fail(w, 400, errors.New("该站点目录由面板之外管理"))
		return
	}
	var input struct {
		Path string `json:"path"`
	}
	if err := decodeJSONBody(w, r, &input); err != nil {
		return
	}
	full, err := resolveWithinRoot(root, input.Path)
	if err != nil {
		fail(w, 400, err)
		return
	}
	if full == root {
		fail(w, 400, errors.New("路径无效"))
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
	writeJSON(w, 201, map[string]any{"path": relPath(root, full)})
}
