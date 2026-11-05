package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
)

// Stored revisions are redacted before they are written, so they cannot be
// replayed into Caddy: secrets such as basic-auth hashes were replaced with a
// placeholder. What they are good for is answering "what changed between these
// two applies?", which is what this diff provides. Actual rollback goes through
// the encrypted config archive, which is the only copy that is complete.
const (
	configDiffMaxPaths = 5000
	configDiffMaxItems = 200
)

type configDiffItem struct {
	Path string `json:"path"`
	From string `json:"from,omitempty"`
	To   string `json:"to,omitempty"`
}

type configDiff struct {
	From      int64            `json:"from"`
	To        int64            `json:"to"`
	Added     []configDiffItem `json:"added"`
	Removed   []configDiffItem `json:"removed"`
	Changed   []configDiffItem `json:"changed"`
	Truncated bool             `json:"truncated"`
}

// flattenJSON turns a config tree into path -> rendered value pairs.
func flattenJSON(prefix string, value any, out map[string]string) bool {
	switch item := value.(type) {
	case map[string]any:
		for key, child := range item {
			path := key
			if prefix != "" {
				path = prefix + "." + key
			}
			if !flattenJSON(path, child, out) {
				return false
			}
		}
	case []any:
		for index, child := range item {
			path := fmt.Sprintf("%s[%d]", prefix, index)
			if !flattenJSON(path, child, out) {
				return false
			}
		}
	default:
		if len(out) >= configDiffMaxPaths {
			return false
		}
		out[prefix] = renderDiffValue(value)
	}
	return true
}

// renderDiffValue keeps scalars readable and keeps nested leaves comparable
// without embedding a whole subtree in one cell.
func renderDiffValue(value any) string {
	switch item := value.(type) {
	case nil:
		return "null"
	case bool:
		return strconv.FormatBool(item)
	case float64:
		return strconv.FormatFloat(item, 'f', -1, 64)
	case string:
		return item
	default:
		encoded, err := json.Marshal(value)
		if err != nil {
			return "<unprintable>"
		}
		return string(encoded)
	}
}

func (a *App) loadRevisionConfig(r *http.Request, id int64) (map[string]string, bool, error) {
	var raw string
	if err := a.db.QueryRowContext(r.Context(), `SELECT config_json FROM config_revisions WHERE id=?`, id).Scan(&raw); err != nil {
		return nil, false, err
	}
	var value any
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		return nil, false, errors.New("已保存的修订无法读取")
	}
	flat := map[string]string{}
	complete := flattenJSON("", value, flat)
	return flat, complete, nil
}

// configRevisionDiff compares two stored revisions. "current" resolves to the
// newest applied snapshot, which is the comparison operators actually want.
func (a *App) configRevisionDiff(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		fail(w, http.StatusBadRequest, errors.New("修订 ID 无效"))
		return
	}
	againstRaw := strings.TrimSpace(r.URL.Query().Get("against"))
	if againstRaw == "" {
		againstRaw = "current"
	}
	var against int64
	if againstRaw == "current" {
		if err := a.db.QueryRowContext(r.Context(), `SELECT id FROM config_revisions WHERE status='applied' ORDER BY id DESC LIMIT 1`).Scan(&against); err != nil {
			fail(w, http.StatusNotFound, errors.New("没有可对比的已应用修订"))
			return
		}
	} else {
		against, err = strconv.ParseInt(againstRaw, 10, 64)
		if err != nil || against < 1 {
			fail(w, http.StatusBadRequest, errors.New("对比修订无效"))
			return
		}
	}
	base, baseOK, err := a.loadRevisionConfig(r, against)
	if err != nil {
		fail(w, http.StatusNotFound, errors.New("对比修订不存在"))
		return
	}
	target, targetOK, err := a.loadRevisionConfig(r, id)
	if err != nil {
		fail(w, http.StatusNotFound, errors.New("修订不存在"))
		return
	}

	result := configDiff{From: against, To: id}
	paths := make([]string, 0, len(base))
	seen := map[string]bool{}
	for path := range base {
		if !seen[path] {
			seen[path] = true
			paths = append(paths, path)
		}
	}
	for path := range target {
		if !seen[path] {
			seen[path] = true
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	for _, path := range paths {
		from, hadFrom := base[path]
		to, hadTo := target[path]
		switch {
		case !hadFrom && hadTo:
			result.Added = append(result.Added, configDiffItem{Path: path, To: to})
		case hadFrom && !hadTo:
			result.Removed = append(result.Removed, configDiffItem{Path: path, From: from})
		case hadFrom && hadTo && from != to:
			result.Changed = append(result.Changed, configDiffItem{Path: path, From: from, To: to})
		}
	}
	result.Truncated = !baseOK || !targetOK
	// Keep the response bounded; a large config can differ in thousands of
	// leaves and the panel only needs enough to explain the change.
	if len(result.Added) > configDiffMaxItems {
		result.Added = result.Added[:configDiffMaxItems]
		result.Truncated = true
	}
	if len(result.Removed) > configDiffMaxItems {
		result.Removed = result.Removed[:configDiffMaxItems]
		result.Truncated = true
	}
	if len(result.Changed) > configDiffMaxItems {
		result.Changed = result.Changed[:configDiffMaxItems]
		result.Truncated = true
	}
	writeJSON(w, http.StatusOK, result)
}

// configRevision returns one stored snapshot so the panel can show it.
func (a *App) configRevision(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		fail(w, http.StatusBadRequest, errors.New("修订 ID 无效"))
		return
	}
	var raw, status string
	if err := a.db.QueryRowContext(r.Context(), `SELECT status,config_json FROM config_revisions WHERE id=?`, id).Scan(&status, &raw); err != nil {
		fail(w, http.StatusNotFound, errors.New("修订不存在"))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(raw))
}
