package main

import (
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Login audit keeps a bounded, append-only trail of authentication attempts so
// a takeover can be reconstructed after the fact. It deliberately stores no
// password material and no session token: only who, from where, how, and when.
const (
	loginAuditRetention = 2000
	loginAuditDays      = 90
	loginAuditUsername  = 64
	loginAuditAgent     = 160
	loginAuditDefault   = 200
	loginAuditMax       = 500
)

// Audit results are a closed vocabulary so the UI can rely on them.
const (
	auditSuccess = "success"
	auditFailure = "failure"
	auditBlocked = "blocked"
)

// Audit reasons explain the outcome without leaking whether an account exists
// to anyone but the authenticated administrator reading this page.
const (
	auditReasonPassword     = "password"
	auditReasonUnknownUser  = "unknown_user"
	auditReasonRateLimited  = "rate_limited"
	auditReasonSetup        = "setup"
	auditReasonLogout       = "logout"
	auditReasonPasswordEdit = "password_changed"
	auditReasonSessionOff   = "sessions_revoked"
	auditReasonTOTP         = "totp"
)

type loginAuditEntry struct {
	ID        int64  `json:"id"`
	Username  string `json:"username"`
	IP        string `json:"ip"`
	UserAgent string `json:"user_agent"`
	Result    string `json:"result"`
	Reason    string `json:"reason"`
	CreatedAt int64  `json:"created_at"`
}

// recordLoginAudit writes one attempt. Auditing must never break the request it
// describes, so every failure here is logged and swallowed.
func (a *App) recordLoginAudit(r *http.Request, username, result, reason string) {
	if a == nil || a.db == nil {
		return
	}
	username = clipText(strings.TrimSpace(username), loginAuditUsername)
	agent := clipText(strings.TrimSpace(r.Header.Get("User-Agent")), loginAuditAgent)
	_, err := a.db.ExecContext(r.Context(),
		`INSERT INTO login_audit(username,ip,user_agent,result,reason,created_at) VALUES(?,?,?,?,?,?)`,
		username, clipText(remoteIP(r), 64), agent, result, reason, time.Now().Unix())
	if err != nil {
		slog.Warn("record login audit", "error", err)
		return
	}
	a.pruneLoginAudit()
}

// pruneLoginAudit trims the table by age and by count. It only runs once the
// table has outgrown its cap, so the common path stays a single insert.
func (a *App) pruneLoginAudit() {
	var count int
	if err := a.db.QueryRow(`SELECT COUNT(*) FROM login_audit`).Scan(&count); err != nil || count <= loginAuditRetention {
		return
	}
	if _, err := a.db.Exec(`DELETE FROM login_audit WHERE created_at < ?`, time.Now().AddDate(0, 0, -loginAuditDays).Unix()); err != nil {
		slog.Warn("prune login audit by age", "error", err)
	}
	if _, err := a.db.Exec(`DELETE FROM login_audit WHERE id IN (SELECT id FROM login_audit ORDER BY id DESC LIMIT -1 OFFSET ?)`, loginAuditRetention); err != nil {
		slog.Warn("prune login audit by count", "error", err)
	}
}

func (a *App) listLoginAudit(w http.ResponseWriter, r *http.Request) {
	limit := loginAuditDefault
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if value, err := strconv.Atoi(raw); err == nil && value > 0 && value <= loginAuditMax {
			limit = value
		}
	}
	rows, err := a.db.QueryContext(r.Context(),
		`SELECT id,username,ip,user_agent,result,reason,created_at FROM login_audit ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		fail(w, http.StatusInternalServerError, errors.New("login audit unavailable"))
		return
	}
	defer rows.Close()
	entries := make([]loginAuditEntry, 0, 32)
	for rows.Next() {
		var entry loginAuditEntry
		if err := rows.Scan(&entry.ID, &entry.Username, &entry.IP, &entry.UserAgent, &entry.Result, &entry.Reason, &entry.CreatedAt); err != nil {
			fail(w, http.StatusInternalServerError, errors.New("login audit unavailable"))
			return
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil && err != sql.ErrNoRows {
		fail(w, http.StatusInternalServerError, errors.New("login audit unavailable"))
		return
	}
	writeJSON(w, http.StatusOK, entries)
}

// clipText cuts a user-supplied string to a byte budget without splitting a
// UTF-8 rune, so truncation cannot produce invalid JSON or a broken glyph.
func clipText(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	cut := limit
	for cut > 0 && value[cut]&0xC0 == 0x80 {
		cut--
	}
	return strings.ToValidUTF8(value[:cut], "")
}
