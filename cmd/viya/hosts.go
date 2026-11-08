package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

func (a *App) listHosts(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.QueryContext(r.Context(), `SELECT `+hostReadColumns+` FROM proxy_hosts ORDER BY domain`)
	if err != nil {
		fail(w, 500, err)
		return
	}
	defer rows.Close()
	result := []Host{}
	for rows.Next() {
		h, scanErr := scanHost(rows)
		if scanErr != nil {
			fail(w, 500, scanErr)
			return
		}
		result = append(result, *h)
	}
	if err := rows.Err(); err != nil {
		fail(w, 500, err)
		return
	}
	global, domains := a.readSecurityGlobal(r.Context()), a.readSecurityDomains(r.Context())
	for i := range result {
		effectiveHostPolicy(&result[i], global, domains)
	}
	a.attachSiteMetadata(r.Context(), result)
	sources, err := a.certificateSources(r.Context(), result)
	if err != nil {
		fail(w, 500, err)
		return
	}
	for i := range result {
		source := sources[result[i].ID]
		result[i].CertificateSource = &source
	}
	writeJSON(w, 200, result)
}

func (a *App) reapplyConfig(w http.ResponseWriter, r *http.Request) {
	if err := a.apply(r.Context()); err != nil {
		fail(w, http.StatusBadGateway, errors.New("Caddy 仍未接受当前配置，请查看程序日志排查"))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) testUpstream(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Upstream string `json:"upstream"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&input); err != nil {
		fail(w, http.StatusBadRequest, errors.New("上游测试请求无效"))
		return
	}
	u, err := parseUpstream(input.Upstream)
	if err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	started := time.Now()
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", net.JoinHostPort(u.Hostname(), u.Port()))
	if err != nil {
		fail(w, http.StatusBadGateway, errors.New("无法连接上游端口，请确认地址正确且目标服务正在运行"))
		return
	}
	_ = conn.Close()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "duration_ms": time.Since(started).Milliseconds()})
}

func (a *App) createHost(w http.ResponseWriter, r *http.Request) {
	var h Host
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&h); err != nil {
		fail(w, 400, errors.New("请求内容不是有效的 JSON"))
		return
	}
	if err := validateHost(&h); err != nil {
		fail(w, 400, err)
		return
	}
	if err := a.validateHostListener(r.Context(), &h); err != nil {
		fail(w, 400, err)
		return
	}
	if err := a.validateTLSSettings(r.Context(), &h); err != nil {
		fail(w, 400, err)
		return
	}
	if err := validateHeaders(h.CustomHeaders); err != nil {
		fail(w, 400, err)
		return
	}
	if err := validateHostSecurity(&h); err != nil {
		fail(w, 400, err)
		return
	}
	if err := validateSecurityHeaders(h.SecurityHeaders); err != nil {
		fail(w, 400, err)
		return
	}
	a.prepareHostPolicy(r.Context(), &h)
	if h.BasicAuthEnabled {
		if len(h.BasicAuthPassword) < 8 || len(h.BasicAuthPassword) > 72 {
			fail(w, 400, errors.New("Basic Auth 密码需为 8-72 字节"))
			return
		}
		hash, err := hashPassword(h.BasicAuthPassword)
		if err != nil {
			fail(w, 500, errors.New("Basic Auth 密码哈希失败"))
			return
		}
		h.PasswordHash = hash
	}
	if err := a.prepareSiteStorage(&h); err != nil {
		fail(w, 400, err)
		return
	}
	res, err := a.db.ExecContext(r.Context(), hostInsertSQL(), hostWriteValues(h)...)
	if err != nil {
		status, mapped := classifyHostWriteError(err)
		fail(w, status, mapped)
		return
	}
	h.ID, _ = res.LastInsertId()
	if err := a.apply(r.Context()); err != nil {
		slog.Error("Caddy proxy-host apply failed; inspect the program logs")
		failSavedApplication(w)
		return
	}
	h.BasicAuthPassword = ""
	h.PasswordHash = ""
	writeJSON(w, 201, h)
}

func (a *App) updateHost(w http.ResponseWriter, r *http.Request) {
	var h Host
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&h); err != nil {
		fail(w, 400, errors.New("请求内容不是有效的 JSON"))
		return
	}
	if err := validateHost(&h); err != nil {
		fail(w, 400, err)
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		fail(w, 400, errors.New("站点 ID 无效"))
		return
	}
	if err := a.validateHostListener(r.Context(), &h); err != nil {
		fail(w, 400, err)
		return
	}
	if err := a.validateTLSSettings(r.Context(), &h); err != nil {
		fail(w, 400, err)
		return
	}
	if err := validateHeaders(h.CustomHeaders); err != nil {
		fail(w, 400, err)
		return
	}
	if err := validateHostSecurity(&h); err != nil {
		fail(w, 400, err)
		return
	}
	if err := validateSecurityHeaders(h.SecurityHeaders); err != nil {
		fail(w, 400, err)
		return
	}
	a.prepareHostPolicy(r.Context(), &h)
	if err := a.prepareSiteStorage(&h); err != nil {
		fail(w, 400, err)
		return
	}
	var storedHash string
	if h.BasicAuthEnabled {
		if h.BasicAuthPassword != "" {
			if len(h.BasicAuthPassword) < 8 || len(h.BasicAuthPassword) > 72 {
				fail(w, 400, errors.New("Basic Auth 密码需为 8-72 字节"))
				return
			}
			storedHash, err = hashPassword(h.BasicAuthPassword)
			if err != nil {
				fail(w, 500, errors.New("Basic Auth 密码哈希失败"))
				return
			}
		} else {
			if err := a.db.QueryRowContext(r.Context(), `SELECT basic_auth_hash FROM proxy_hosts WHERE id=?`, id).Scan(&storedHash); err != nil || storedHash == "" {
				fail(w, 400, errors.New("请填写 Basic Auth 密码"))
				return
			}
		}
	}
	h.PasswordHash = storedHash
	res, err := a.db.ExecContext(r.Context(), hostUpdateSQL(), append(hostWriteValues(h), id, h.Version)...)
	if err != nil {
		status, mapped := classifyHostWriteError(err)
		fail(w, status, mapped)
		return
	}
	changed, err := res.RowsAffected()
	if err != nil {
		fail(w, 500, err)
		return
	}
	if changed == 0 {
		var exists int
		if queryErr := a.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM proxy_hosts WHERE id=?`, id).Scan(&exists); queryErr != nil {
			fail(w, 500, queryErr)
			return
		}
		if exists == 0 {
			fail(w, 404, errors.New("站点不存在"))
			return
		}
		fail(w, 409, errors.New("该站点已被其他会话修改，请刷新后重新编辑"))
		return
	}
	if err := a.apply(r.Context()); err != nil {
		slog.Error("Caddy proxy-host apply failed; inspect the program logs")
		failSavedApplication(w)
		return
	}
	h.BasicAuthPassword = ""
	h.PasswordHash = ""
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (a *App) deleteHost(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		fail(w, 400, errors.New("站点 ID 无效"))
		return
	}
	tx, err := a.db.BeginTx(r.Context(), nil)
	if err != nil {
		fail(w, 500, err)
		return
	}
	defer tx.Rollback()
	var siteType, domain, rootPath string
	if err := tx.QueryRowContext(r.Context(), `SELECT type,domain,root_path FROM proxy_hosts WHERE id=?`, id).Scan(&siteType, &domain, &rootPath); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			fail(w, 404, errors.New("站点不存在"))
		} else {
			fail(w, 500, errors.New("无法读取站点"))
		}
		return
	}
	deleteSiteFiles := siteType == "static" && r.URL.Query().Get("delete_files") == "1"
	sharedSiteFiles := false
	if deleteSiteFiles {
		if strings.TrimSpace(rootPath) == "" {
			rootPath = managedStaticSiteRoot(a, domain)
		}
		rootPath = filepath.Clean(strings.TrimSpace(rootPath))
		rows, queryErr := tx.QueryContext(r.Context(), `SELECT domain,root_path FROM proxy_hosts WHERE type='static' AND id<>?`, id)
		if queryErr != nil {
			fail(w, 500, errors.New("无法检查共享静态站点文件"))
			return
		}
		for rows.Next() {
			var otherDomain, otherRoot string
			if scanErr := rows.Scan(&otherDomain, &otherRoot); scanErr != nil {
				_ = rows.Close()
				fail(w, 500, errors.New("无法检查共享静态站点文件"))
				return
			}
			otherRoot = strings.TrimSpace(otherRoot)
			if otherRoot == "" {
				otherRoot = managedStaticSiteRoot(a, otherDomain)
			} else {
				otherRoot = filepath.Clean(otherRoot)
			}
			if otherRoot == rootPath || (runtime.GOOS == "windows" && strings.EqualFold(otherRoot, rootPath)) {
				sharedSiteFiles = true
				break
			}
		}
		if rowsErr := rows.Err(); rowsErr != nil {
			_ = rows.Close()
			fail(w, 500, errors.New("无法检查共享静态站点文件"))
			return
		}
		_ = rows.Close()
		if !sharedSiteFiles {
			if validateErr := a.validateManagedStaticSiteRoot(rootPath); validateErr != nil {
				fail(w, 400, errors.New("静态站点文件位于面板托管目录外或路径不安全；请勾选不要删除网站文件后重试"))
				return
			}
		}
	}
	res, err := tx.ExecContext(r.Context(), `DELETE FROM proxy_hosts WHERE id=?`, id)
	if err != nil {
		fail(w, 500, err)
		return
	}
	changed, err := res.RowsAffected()
	if err != nil || changed == 0 {
		fail(w, 404, errors.New("站点不存在"))
		return
	}
	if _, err = tx.ExecContext(r.Context(), `DELETE FROM panel_settings WHERE key=?`, "site_meta:"+strconv.FormatInt(id, 10)); err != nil {
		fail(w, 500, err)
		return
	}
	if deleteSiteFiles && !sharedSiteFiles {
		if _, err = tx.ExecContext(r.Context(), `INSERT OR IGNORE INTO pending_file_deletions(root_path) VALUES(?)`, rootPath); err != nil {
			fail(w, 500, err)
			return
		}
	}
	if err = tx.Commit(); err != nil {
		fail(w, 500, err)
		return
	}
	if err := a.apply(r.Context()); err != nil {
		slog.Error("Caddy proxy-host apply failed; inspect the program logs")
		failSavedApplication(w)
		return
	}
	if deleteSiteFiles && !sharedSiteFiles {
		if err := a.removeManagedStaticSite(rootPath); err != nil {
			slog.Error("could not remove deleted static site files", "error", err)
			fail(w, 500, errors.New("站点已删除，但静态文件未能移除"))
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// validateStaticRoot checks a static site root: it must be an absolute path
// with no parent-directory segments. The directory itself may not exist yet;
// prepareSiteStorage creates it when the panel manages it.

// validateStaticRoot checks a static site root: it must be an absolute path
// with no parent-directory segments. The directory itself may not exist yet;
// prepareSiteStorage creates it when the panel manages it.
func validateStaticRoot(root string) error {
	root = strings.TrimSpace(root)
	if root == "" {
		return errors.New("请填写静态站点根目录")
	}
	if !filepath.IsAbs(root) {
		return errors.New("静态站点根目录必须是绝对路径")
	}
	if strings.ContainsRune(root, 0) {
		return errors.New("静态站点根目录包含无效字符")
	}
	for _, segment := range strings.FieldsFunc(root, func(r rune) bool { return r == '/' || r == '\\' }) {
		if segment == ".." {
			return errors.New("静态站点根目录不能包含 .. 路径段")
		}
	}
	return nil
}

// managedSitesDir is the only tree the panel may write into (the systemd unit
// gives the service write access to its data directory alone). File uploads
// are accepted only for static sites rooted here.

// managedSitesDir is the only tree the panel may write into (the systemd unit
// gives the service write access to its data directory alone). File uploads
// are accepted only for static sites rooted here.
func (a *App) managedSitesDir() string {
	return filepath.Join(a.dataDir, "sites")
}

func managedStaticSiteRoot(a *App, domain string) string {
	slug := strings.ToLower(domain)
	var b strings.Builder
	for _, r := range slug {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '.' {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	name := strings.Trim(b.String(), "-.")
	if name == "" {
		name = "site"
	}
	return filepath.Join(a.managedSitesDir(), name)
}

// prepareSiteStorage resolves a static site's root and makes sure it exists.
// Roots inside the managed tree are created on demand; external roots must
// already exist because the service cannot write outside its data directory.

// prepareSiteStorage resolves a static site's root and makes sure it exists.
// Roots inside the managed tree are created on demand; external roots must
// already exist because the service cannot write outside its data directory.
func (a *App) prepareSiteStorage(h *Host) error {
	if h.Type != "static" && h.Type != "files" {
		return nil
	}
	root := strings.TrimSpace(h.RootPath)
	if root == "" {
		slug := strings.ToLower(h.Domain)
		var b strings.Builder
		for _, r := range slug {
			if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '.' {
				b.WriteRune(r)
			} else {
				b.WriteByte('-')
			}
		}
		name := strings.Trim(b.String(), "-.")
		if name == "" {
			name = "site"
		}
		root = filepath.Join(a.managedSitesDir(), name)
		h.RootPath = root
	}
	if err := validateStaticRoot(root); err != nil {
		return err
	}
	h.RootPath = filepath.Clean(root)
	if err := os.MkdirAll(h.RootPath, 0750); err != nil {
		return errors.New("静态站点根目录不可访问，请先在服务器上创建该目录")
	}
	info, err := os.Stat(h.RootPath)
	if err != nil || !info.IsDir() {
		return errors.New("静态站点根目录必须是已存在的目录")
	}
	return nil
}
