package main

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
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
// non-static sites so the file API cannot be pointed at a proxy upstream.
func (a *App) loadStaticSite(r *http.Request) (int64, string, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		return 0, "", errors.New("invalid site id")
	}
	var siteType, root string
	if err := a.db.QueryRowContext(r.Context(), `SELECT type,root_path FROM proxy_hosts WHERE id=?`, id).Scan(&siteType, &root); err != nil {
		return 0, "", errors.New("site not found")
	}
	if siteType != "static" {
		return 0, "", errors.New("file management is only available for static sites")
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
		return "", errors.New("invalid path")
	}
	for _, segment := range strings.FieldsFunc(rel, func(r rune) bool { return r == '/' || r == '\\' }) {
		if segment == ".." {
			return "", errors.New("path escapes the site root")
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
		return "", errors.New("path escapes the site root")
	}
	return full, nil
}

// refuseSymlink walks every existing component below root and rejects symlinks
// so mutations cannot be redirected outside the tree.
func refuseSymlink(root, full string) error {
	rel, err := filepath.Rel(root, full)
	if err != nil {
		return errors.New("invalid path")
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
			return errors.New("symbolic links are not allowed here")
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
		fail(w, 404, errors.New("directory not found"))
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		fail(w, 500, errors.New("could not read directory"))
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
		fail(w, 404, errors.New("file not found"))
		return
	}
	w.Header().Set("Content-Disposition", `attachment; filename="`+filepath.Base(full)+`"`)
	http.ServeFile(w, r, full)
}

// uploadSiteFiles stores uploaded files below the site root. A .zip payload is
// extracted in place instead of being stored, which is what dropping a built
// static site onto the panel usually means.
func (a *App) uploadSiteFiles(w http.ResponseWriter, r *http.Request) {
	_, root, err := a.loadStaticSite(r)
	if err != nil {
		fail(w, 400, err)
		return
	}
	if !a.underManagedRoot(root) {
		fail(w, 400, errors.New("this site's directory is managed outside the panel; upload there instead"))
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
		fail(w, 500, errors.New("could not create target directory"))
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, siteFileMaxRequest)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		fail(w, 400, errors.New("invalid multipart upload (too large?)"))
		return
	}
	form := r.MultipartForm
	if form == nil || len(form.File["files"]) == 0 {
		fail(w, 400, errors.New("no files were uploaded"))
		return
	}
	stored, extracted := 0, 0
	for _, header := range form.File["files"] {
		filename := filepath.Base(filepath.FromSlash(header.Filename))
		if filename == "." || filename == ".." || filename == "" {
			fail(w, 400, errors.New("invalid file name"))
			return
		}
		src, err := header.Open()
		if err != nil {
			fail(w, 500, errors.New("could not read uploaded file"))
			return
		}
		if strings.EqualFold(filepath.Ext(filename), ".zip") {
			count, err := extractZip(src, target, siteZipMaxEntries, siteZipMaxUnpacked)
			src.Close()
			if err != nil {
				fail(w, 400, fmt.Errorf("zip %s: %w", filename, err))
				return
			}
			extracted += count
			continue
		}
		dstPath := filepath.Join(target, filename)
		if err := refuseSymlink(root, dstPath); err != nil {
			src.Close()
			fail(w, 400, err)
			return
		}
		dst, err := os.OpenFile(dstPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, siteFileMode)
		if err != nil {
			src.Close()
			fail(w, 500, errors.New("could not write file"))
			return
		}
		_, copyErr := io.Copy(dst, src)
		src.Close()
		closeErr := dst.Close()
		if copyErr != nil || closeErr != nil {
			_ = os.Remove(dstPath)
			fail(w, 500, errors.New("could not store file"))
			return
		}
		stored++
	}
	writeJSON(w, 201, map[string]any{"stored": stored, "extracted": extracted})
}

// extractZip unpacks an archive below dest with zip-slip and bomb guards.
func extractZip(src io.Reader, dest string, maxEntries int, maxBytes int64) (int, error) {
	// Buffer into a temp file: zip needs random access and the payload may be
	// large. The reader is already capped by http.MaxBytesReader upstream.
	tmp, err := os.CreateTemp("", "viya-upload-*.zip")
	if err != nil {
		return 0, errors.New("could not stage archive")
	}
	defer os.Remove(tmp.Name())
	if _, err := io.Copy(tmp, src); err != nil {
		tmp.Close()
		return 0, errors.New("could not read archive")
	}
	if err := tmp.Close(); err != nil {
		return 0, errors.New("could not read archive")
	}
	reader, err := zip.OpenReader(tmp.Name())
	if err != nil {
		return 0, errors.New("not a valid zip archive")
	}
	defer reader.Close()
	if len(reader.File) > maxEntries {
		return 0, errors.New("archive has too many entries")
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
				return count, errors.New("could not create directory")
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), siteMkdirMode); err != nil {
			return count, errors.New("could not create directory")
		}
		written += int64(file.UncompressedSize64)
		if written > maxBytes {
			return count, errors.New("archive expands beyond the size limit")
		}
		in, err := file.Open()
		if err != nil {
			return count, errors.New("could not read archive entry")
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, siteFileMode)
		if err != nil {
			in.Close()
			return count, errors.New("could not write archive entry")
		}
		_, copyErr := io.Copy(out, in)
		in.Close()
		closeErr := out.Close()
		if copyErr != nil || closeErr != nil {
			_ = os.Remove(target)
			return count, errors.New("could not store archive entry")
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
		fail(w, 400, errors.New("this site's directory is managed outside the panel"))
		return
	}
	if err := refuseSymlink(root, full); err != nil {
		fail(w, 400, err)
		return
	}
	if full == root {
		fail(w, 400, errors.New("refusing to delete the site root"))
		return
	}
	info, err := os.Lstat(full)
	if err != nil {
		fail(w, 404, errors.New("not found"))
		return
	}
	if !info.IsDir() {
		if err := os.Remove(full); err != nil {
			fail(w, 500, errors.New("could not delete file"))
			return
		}
		writeJSON(w, 200, map[string]any{"deleted": 1})
		return
	}
	if r.URL.Query().Get("recursive") != "1" {
		entries, err := os.ReadDir(full)
		if err != nil {
			fail(w, 500, errors.New("could not read directory"))
			return
		}
		if len(entries) > 0 {
			fail(w, 400, errors.New("directory is not empty; confirm recursive delete"))
			return
		}
		if err := os.Remove(full); err != nil {
			fail(w, 500, errors.New("could not delete directory"))
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
			return errors.New("directory has too many entries to delete at once")
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	if err := os.RemoveAll(root); err != nil {
		return 0, errors.New("could not delete directory")
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
		fail(w, 400, errors.New("this site's directory is managed outside the panel"))
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
		fail(w, 400, errors.New("invalid path"))
		return
	}
	if err := refuseSymlink(root, full); err != nil {
		fail(w, 400, err)
		return
	}
	if err := os.MkdirAll(full, siteMkdirMode); err != nil {
		fail(w, 500, errors.New("could not create directory"))
		return
	}
	writeJSON(w, 201, map[string]any{"path": relPath(root, full)})
}
