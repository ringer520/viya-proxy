package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// A bounded, append-only trail of successful configuration mutations. Unlike
// the retired config_revisions table this stores one short row per action,
// never a full Caddy snapshot, and is excluded from encrypted exports.
const (
	configAuditRetention = 2000
	configAuditDays      = 90
	configAuditDefault   = 200
	configAuditMax       = 500
)

type configAuditEntry struct {
	ID        int64  `json:"id"`
	Username  string `json:"username"`
	Method    string `json:"method"`
	Path      string `json:"path"`
	Summary   string `json:"summary"`
	Status    int    `json:"status"`
	RequestID string `json:"request_id"`
	CreatedAt int64  `json:"created_at"`
}

// recordConfigAudit runs after the handler returns, so failed writes are not
// recorded as changes. It reuses the operation summary requireSession already
// computes and the request ID issued by requestIDMiddleware.
func (a *App) recordConfigAudit(ctx context.Context, actor, method, path string, status int) {
	if a == nil || a.db == nil || !isWriteRequest(method) || status < 200 || status >= 300 {
		return
	}
	// Authentication events have their own richer login_audit table.
	if strings.HasPrefix(path, "/api/v1/auth/") {
		return
	}
	op, ok := ctx.Value(operationContextKey{}).(operationContext)
	if !ok {
		return
	}
	_, err := a.db.ExecContext(ctx,
		`INSERT INTO config_audit(username,method,path,summary,status,request_id,created_at) VALUES(?,?,?,?,?,?,?)`,
		clipText(strings.TrimSpace(actor), 64), method, clipText(path, 200),
		clipText(op.Summary, 200), status, requestIDFrom(ctx), time.Now().Unix())
	if err != nil {
		slog.Warn("record config audit", "error", err)
		return
	}
	a.pruneConfigAudit()
}

func (a *App) pruneConfigAudit() {
	var count int
	if err := a.db.QueryRow(`SELECT COUNT(*) FROM config_audit`).Scan(&count); err != nil || count <= configAuditRetention {
		return
	}
	if _, err := a.db.Exec(`DELETE FROM config_audit WHERE created_at < ?`, time.Now().AddDate(0, 0, -configAuditDays).Unix()); err != nil {
		slog.Warn("prune config audit by age", "error", err)
	}
	if _, err := a.db.Exec(`DELETE FROM config_audit WHERE id IN (SELECT id FROM config_audit ORDER BY id DESC LIMIT -1 OFFSET ?)`, configAuditRetention); err != nil {
		slog.Warn("prune config audit by count", "error", err)
	}
}

func (a *App) listConfigAudit(w http.ResponseWriter, r *http.Request) {
	limit := configAuditDefault
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if value, err := strconv.Atoi(raw); err == nil && value > 0 && value <= configAuditMax {
			limit = value
		}
	}
	query, err := parseLogQuery(r)
	if err != nil {
		fail(w, 400, err)
		return
	}
	rows, err := a.db.QueryContext(r.Context(),
		`SELECT id,username,method,path,summary,status,request_id,created_at FROM config_audit
		 WHERE (?=0 OR created_at>=?) AND (?=0 OR created_at<=?)
		   AND (?='' OR instr(lower(username||' '||path||' '||summary||' '||request_id),?)>0)
		 ORDER BY id DESC LIMIT ?`,
		query.From, query.From, query.To, query.To, query.Text, query.Text, limit)
	if err != nil {
		fail(w, http.StatusInternalServerError, errors.New("无法获取配置变更审计"))
		return
	}
	defer rows.Close()
	entries := make([]configAuditEntry, 0, 32)
	for rows.Next() {
		var e configAuditEntry
		if err := rows.Scan(&e.ID, &e.Username, &e.Method, &e.Path, &e.Summary, &e.Status, &e.RequestID, &e.CreatedAt); err != nil {
			fail(w, http.StatusInternalServerError, errors.New("无法获取配置变更审计"))
			return
		}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		fail(w, http.StatusInternalServerError, errors.New("无法获取配置变更审计"))
		return
	}
	writeJSON(w, http.StatusOK, entries)
}
