package main

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

var automaticBackupName = regexp.MustCompile(`^auto-[0-9]{4}-[0-9]{2}-[0-9]{2}$`)

func (a *App) archiveDailyCheckpoint() error {
	raw, e := os.ReadFile(a.checkpointPath("current"))
	if os.IsNotExist(e) {
		return nil
	}
	if e != nil {
		return e
	}
	name := "auto-" + time.Now().UTC().Format("2006-01-02")
	if e = atomicRecoveryFile(a.checkpointPath(name), raw); e != nil {
		return e
	}
	files, e := os.ReadDir(filepath.Dir(a.checkpointPath("current")))
	if e != nil {
		return e
	}
	names := []string{}
	for _, f := range files {
		if !f.IsDir() && automaticBackupName.MatchString(strings.TrimSuffix(f.Name(), ".json")) {
			names = append(names, f.Name())
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(names)))
	// Keep only the five most recent daily recovery points.
	for i := 5; i < len(names); i++ {
		if e = os.Remove(filepath.Join(a.dataDir, "recovery", names[i])); e != nil {
			return e
		}
	}
	return nil
}
func (a *App) watchRecoveryBackups() {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-a.ctx.Done():
			return
		case <-ticker.C:
			a.mu.Lock()
			_ = a.archiveDailyCheckpoint()
			a.mu.Unlock()
		}
	}
}
func (a *App) listRecoveryBackups(w http.ResponseWriter, r *http.Request) {
	files, e := os.ReadDir(filepath.Join(a.dataDir, "recovery"))
	if e != nil && !os.IsNotExist(e) {
		fail(w, 500, errors.New("无法读取恢复点列表"))
		return
	}
	backups := []map[string]any{}
	for _, f := range files {
		name := strings.TrimSuffix(f.Name(), ".json")
		if !automaticBackupName.MatchString(name) || f.Type()&os.ModeSymlink != 0 {
			continue
		}
		info, e := f.Info()
		if e == nil {
			backups = append(backups, map[string]any{"name": name, "updated": info.ModTime().UTC().Format(time.RFC3339), "size": info.Size()})
		}
	}
	sort.Slice(backups, func(i, j int) bool { return backups[i]["name"].(string) > backups[j]["name"].(string) })
	writeJSON(w, 200, backups)
}
