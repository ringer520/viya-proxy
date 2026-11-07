package main

import (
	"database/sql"
	"errors"
	"net/http"
)

// The dedicated WebDAV dialog is retired. It always published a plain-HTTP
// share (tls_mode=none, no listener, no directory listing) and could only ever
// manage one host because it looked the share up with LIMIT 1. Shares are now
// created as ordinary "files" hosts through the site form, which supports HTTPS
// listeners, managed or custom certificates, multiple instances and the regular
// access controls.
//
// GET stays for one release so an older cached frontend can still render its
// read-only view; PUT is gone and points the caller at the new flow.

func (a *App) getWebDAVSettings(w http.ResponseWriter, r *http.Request) {
	var id int64
	var domain, rootPath, user, tlsMode, listenerID string
	var webdavWrite int
	err := a.db.QueryRowContext(r.Context(), `SELECT id,domain,root_path,basic_auth_user,tls_mode,listener_id,webdav_write FROM proxy_hosts WHERE type='files' ORDER BY id LIMIT 1`).
		Scan(&id, &domain, &rootPath, &user, &tlsMode, &listenerID, &webdavWrite)
	if errors.Is(err, sql.ErrNoRows) {
		writeJSON(w, 200, map[string]any{"configured": false})
		return
	}
	if err != nil {
		fail(w, 500, errors.New("读取配置失败"))
		return
	}
	writeJSON(w, 200, map[string]any{
		"configured":      true,
		"deprecated":      true,
		"id":              id,
		"domain":          domain,
		"root_path":       rootPath,
		"basic_auth_user": user,
		"tls_mode":        tlsMode,
		"listener_id":     listenerID,
		"webdav_write":    webdavWrite == 1,
	})
}

func (a *App) saveWebDAVSettings(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Deprecation", "true")
	w.Header().Set("Sunset", "Wed, 31 Dec 2026 23:59:59 GMT")
	fail(w, 410, errors.New("WebDAV 设置已退役，请在「文件与共享」中新建文件服务站点"))
}
