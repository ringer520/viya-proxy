package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type retentionSettings struct {
	TrafficDays  int `json:"traffic_days"`
	AccessEvents int `json:"access_events"`
}

func (a *App) retention(ctx context.Context) retentionSettings {
	v := retentionSettings{90, 100}
	var raw string
	if a.db.QueryRowContext(ctx, `SELECT value FROM panel_settings WHERE key='retention'`).Scan(&raw) == nil {
		var saved retentionSettings
		if json.Unmarshal([]byte(raw), &saved) == nil && validRetention(saved) {
			v = saved
		}
	}
	return v
}
func validRetention(v retentionSettings) bool {
	return v.TrafficDays >= 30 && v.TrafficDays <= 365 && v.AccessEvents >= 0 && v.AccessEvents <= 100
}

type storageCategory struct {
	Name  string `json:"name"`
	Bytes int64  `json:"bytes"`
	Files int    `json:"files"`
}

func storageGroup(rel string) string {
	name := filepath.Base(rel)
	switch {
	case strings.HasPrefix(rel, "sites/"):
		return "sites"
	case strings.HasPrefix(rel, "recovery/"):
		return "recovery"
	case strings.HasPrefix(rel, "custom-certs/") || strings.HasPrefix(rel, ".local/") || strings.HasPrefix(rel, ".config/"):
		return "certificates"
	case strings.HasSuffix(name, ".db") || strings.HasSuffix(name, ".db-wal") || strings.HasSuffix(name, ".db-shm"):
		return "database"
	case strings.HasSuffix(name, ".log") || strings.HasSuffix(name, ".log.gz") || strings.HasPrefix(name, "access-") || strings.HasPrefix(name, "program-") || strings.HasPrefix(name, "l4-"):
		return "logs"
	default:
		return "other"
	}
}
func (a *App) storageUsage(w http.ResponseWriter, r *http.Request) {
	groups := map[string]*storageCategory{}
	for _, name := range []string{"database", "sites", "certificates", "logs", "recovery", "other"} {
		groups[name] = &storageCategory{Name: name}
	}
	skipped := 0
	err := filepath.WalkDir(a.dataDir, func(path string, entry fs.DirEntry, err error) error {
		if r.Context().Err() != nil {
			return r.Context().Err()
		}
		if err != nil {
			skipped++
			return nil
		}
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			skipped++
			return nil
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(a.dataDir, path)
		if err != nil {
			return err
		}
		group := groups[storageGroup(filepath.ToSlash(rel))]
		group.Bytes += info.Size()
		group.Files++
		return nil
	})
	if err != nil {
		fail(w, 500, errors.New("无法读取数据目录容量"))
		return
	}
	out := []storageCategory{}
	var total int64
	for _, name := range []string{"database", "sites", "certificates", "logs", "recovery", "other"} {
		out = append(out, *groups[name])
		total += groups[name].Bytes
	}
	writeJSON(w, 200, map[string]any{"categories": out, "total": total, "skipped": skipped, "retention": a.retention(r.Context()), "schema_version": schemaVersion, "external_backups_included": false})
}
func (a *App) saveRetention(w http.ResponseWriter, r *http.Request) {
	var v retentionSettings
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&v) != nil || !validRetention(v) {
		fail(w, 400, errors.New("流量保留 30–365 天，近期访问记录每站点保留 0–100 条"))
		return
	}
	raw, _ := json.Marshal(v)
	if err := writePanelSetting(r.Context(), a, "retention", string(raw)); err != nil {
		fail(w, 500, err)
		return
	}
	a.refreshWorkingCheckpoint(r.Context())
	writeJSON(w, 200, map[string]any{"ok": true})
}
func (a *App) cleanupStorage(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Confirmed bool `json:"confirmed"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&input) != nil || !input.Confirmed {
		fail(w, 400, errors.New("请确认清理过期统计和超出数量的近期访问记录"))
		return
	}
	if err := a.pruneTraffic(r.Context()); err != nil {
		fail(w, 500, err)
		return
	}
	// Bounded caches and expired sessions are disposable. Website files,
	// credentials, certificate storage and recovery points are never removed.
	_, err := a.db.ExecContext(r.Context(), `DELETE FROM sessions WHERE expires_at<?`, time.Now().Unix())
	if err != nil {
		fail(w, 500, err)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "message": "已清理过期记录；SQLite 空闲页将自动复用"})
}

func pruneRecentAccess(ctx context.Context, db *sql.DB, limit int) error {
	_, err := db.ExecContext(ctx, `DELETE FROM site_access_events WHERE id IN (SELECT id FROM (SELECT id,ROW_NUMBER() OVER(PARTITION BY site_id ORDER BY id DESC) AS position FROM site_access_events) WHERE position>?)`, limit)
	return err
}
