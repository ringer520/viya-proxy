package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
)

type siteMetadata struct {
	Domain string   `json:"domain"`
	Tags   []string `json:"tags"`
}

func (a *App) attachSiteMetadata(ctx context.Context, hosts []Host) {
	rows, e := a.db.QueryContext(ctx, `SELECT key,value FROM panel_settings WHERE key LIKE 'site_meta:%'`)
	if e != nil {
		return
	}
	defer rows.Close()
	meta := map[string]siteMetadata{}
	for rows.Next() {
		var key, raw string
		if rows.Scan(&key, &raw) == nil {
			var m siteMetadata
			if json.Unmarshal([]byte(raw), &m) == nil {
				meta[key] = m
			}
		}
	}
	for i := range hosts {
		m := meta["site_meta:"+strconv.FormatInt(hosts[i].ID, 10)]
		if m.Domain == hosts[i].Domain {
			hosts[i].Tags = m.Tags
		}
	}
}
func (a *App) updateSiteMetadata(w http.ResponseWriter, r *http.Request) {
	id, e := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if e != nil || id < 1 {
		fail(w, 400, errors.New("站点 ID 无效"))
		return
	}
	var m siteMetadata
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&m) != nil {
		fail(w, 400, errors.New("元信息无效"))
		return
	}
	if e = a.db.QueryRowContext(r.Context(), `SELECT domain FROM proxy_hosts WHERE id=?`, id).Scan(&m.Domain); e != nil {
		fail(w, 404, errors.New("站点不存在"))
		return
	}
	if len(m.Tags) > 10 {
		fail(w, 400, errors.New("最多 10 个标签"))
		return
	}
	tags := []string{}
	seen := map[string]bool{}
	for _, tag := range m.Tags {
		tag = strings.TrimSpace(tag)
		if len([]rune(tag)) > 24 {
			fail(w, 400, errors.New("标签最多 24 个字符"))
			return
		}
		if tag != "" && !seen[tag] {
			tags = append(tags, tag)
			seen[tag] = true
		}
	}
	m.Tags = tags
	raw, _ := json.Marshal(m)
	if e = writePanelSetting(r.Context(), a, "site_meta:"+strconv.FormatInt(id, 10), string(raw)); e != nil {
		fail(w, 500, e)
		return
	}
	a.refreshWorkingCheckpoint(r.Context())
	writeJSON(w, 200, m)
}
