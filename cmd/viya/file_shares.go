package main

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
)

// fileShareView is the read-only inventory consumed by the Files & Shares hub.
// It deliberately carries no password, password hash, or stored credentials:
// the page only needs to show whether authentication is configured and how a
// WebDAV client connects.
type fileShareView struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Domain      string `json:"domain"`
	Type        string `json:"type"`
	RootPath    string `json:"root_path"`
	Path        string `json:"path"`
	Enabled     bool   `json:"enabled"`
	Browse      bool   `json:"browse"`
	WebDAV      bool   `json:"webdav"`
	WebDAVWrite bool   `json:"webdav_write"`
	AuthEnabled bool   `json:"auth_enabled"`
	AuthUser    string `json:"auth_user"`
	ListenerID  string `json:"listener_id"`
	URL         string `json:"url"`
	TLSMode     string `json:"tls_mode"`
}

// shareURL builds the public address from the listener the site is attached
// to. A non-default port is kept so copy-paste WebDAV addresses stay accurate.
func shareURL(listeners map[string]WebListener, domain, path, listenerID string) string {
	l, ok := listeners[listenerID]
	if !ok {
		return ""
	}
	scheme := l.Protocol
	if scheme != "https" {
		scheme = "http"
	}
	addr := scheme + "://" + domain
	if (scheme == "https" && l.Port != 443) || (scheme == "http" && l.Port != 80) {
		addr += ":" + strconv.Itoa(l.Port)
	}
	p := strings.TrimSpace(path)
	if p != "" && p != "/" {
		if !strings.HasPrefix(p, "/") {
			p = "/" + p
		}
		addr += strings.TrimSuffix(p, "/")
	}
	return addr
}

func (a *App) listFileShares(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.QueryContext(r.Context(), `
		SELECT id,name,domain,type,root_path,path,enabled,browse,webdav,webdav_write,
		       basic_auth_user,basic_auth_hash,listener_id,tls_mode
		FROM proxy_hosts
		WHERE type IN ('static','files')
		ORDER BY type,domain`)
	if err != nil {
		fail(w, 500, errors.New("读取共享列表失败"))
		return
	}
	defer rows.Close()

	shares := []fileShareView{}
	for rows.Next() {
		var s fileShareView
		var enabled, browse, webdav, webdavWrite int
		var authHash string
		if err := rows.Scan(&s.ID, &s.Name, &s.Domain, &s.Type, &s.RootPath, &s.Path,
			&enabled, &browse, &webdav, &webdavWrite, &s.AuthUser, &authHash,
			&s.ListenerID, &s.TLSMode); err != nil {
			fail(w, 500, errors.New("读取共享列表失败"))
			return
		}
		s.Enabled = enabled == 1
		s.Browse = browse == 1
		s.WebDAV = webdav == 1
		s.WebDAVWrite = webdavWrite == 1
		// Report only whether a credential exists; never expose the hash itself.
		s.AuthEnabled = authHash != "" && s.AuthUser != ""
		shares = append(shares, s)
	}
	if err := rows.Err(); err != nil {
		fail(w, 500, errors.New("读取共享列表失败"))
		return
	}

	// Listener metadata is best-effort: a broken listener config must not hide
	// the shares themselves. The URL simply stays empty until it is fixed.
	listenerMap := map[string]WebListener{}
	if listeners, lerr := a.webListeners(r.Context()); lerr == nil {
		for _, l := range listeners {
			listenerMap[l.ID] = l
		}
	}
	for i := range shares {
		shares[i].URL = shareURL(listenerMap, shares[i].Domain, shares[i].Path, shares[i].ListenerID)
	}

	writeJSON(w, 200, map[string]any{"shares": shares})
}
