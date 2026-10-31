package main

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

// Additional accounts exist so a household can hand out read-only access
// without sharing the administrator password. Every write path is already
// closed to viewers by the session middleware; this file only manages the list.
type panelUser struct {
	ID        int64  `json:"id"`
	Username  string `json:"username"`
	Role      string `json:"role"`
	CreatedAt string `json:"created_at"`
}

func validateUsername(value string) string {
	value = strings.TrimSpace(value)
	if len(value) < 3 || len(value) > 64 {
		return ""
	}
	for _, char := range value {
		if !(char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || char == '_' || char == '-' || char == '.') {
			return ""
		}
	}
	return value
}

func (a *App) listUsers(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.QueryContext(r.Context(), `SELECT id,username,COALESCE(role,'admin'),created_at FROM users ORDER BY id`)
	if err != nil {
		fail(w, http.StatusInternalServerError, errors.New("user list unavailable"))
		return
	}
	defer rows.Close()
	out := []panelUser{}
	for rows.Next() {
		var user panelUser
		if err := rows.Scan(&user.ID, &user.Username, &user.Role, &user.CreatedAt); err != nil {
			fail(w, http.StatusInternalServerError, errors.New("user list unavailable"))
			return
		}
		out = append(out, user)
	}
	if err := rows.Err(); err != nil {
		fail(w, http.StatusInternalServerError, errors.New("user list unavailable"))
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *App) createUser(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Role     string `json:"role"`
	}
	if err := decodeJSONBody(w, r, &input); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	username := validateUsername(input.Username)
	if username == "" {
		fail(w, http.StatusBadRequest, errors.New("username must be 3-64 characters of letters, numbers, dot, dash, or underscore"))
		return
	}
	if len(input.Password) < 8 || len(input.Password) > 72 {
		fail(w, http.StatusBadRequest, errors.New("password must be 8-72 bytes"))
		return
	}
	if input.Role != roleViewer && input.Role != roleAdmin {
		fail(w, http.StatusBadRequest, errors.New("role must be admin or viewer"))
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(input.Password), bcrypt.DefaultCost)
	if err != nil {
		fail(w, http.StatusInternalServerError, errors.New("could not create account"))
		return
	}
	var existing int
	if err := a.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM users WHERE username=?`, username).Scan(&existing); err == nil && existing > 0 {
		fail(w, http.StatusConflict, errors.New("that username is already taken"))
		return
	}
	result, err := a.db.ExecContext(r.Context(), `INSERT INTO users(username,password_hash,role) VALUES(?,?,?)`, username, hash, input.Role)
	if err != nil {
		fail(w, http.StatusConflict, errors.New("could not create account"))
		return
	}
	id, _ := result.LastInsertId()
	writeJSON(w, http.StatusCreated, panelUser{ID: id, Username: username, Role: input.Role})
}

func (a *App) updateUser(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		fail(w, http.StatusBadRequest, errors.New("invalid user id"))
		return
	}
	session := r.Context().Value(sessionContextKey{}).(sessionContext)
	var input struct {
		Role     string `json:"role"`
		Password string `json:"password"`
	}
	if err := decodeJSONBody(w, r, &input); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	var selfID int64
	if err := a.db.QueryRowContext(r.Context(), `SELECT id FROM users WHERE username=?`, session.username).Scan(&selfID); err != nil {
		fail(w, http.StatusInternalServerError, errors.New("could not resolve the current account"))
		return
	}
	if input.Role != "" {
		if input.Role != roleViewer && input.Role != roleAdmin {
			fail(w, http.StatusBadRequest, errors.New("role must be admin or viewer"))
			return
		}
		// Losing the last administrator would leave nobody able to manage the
		// panel, so both demoting yourself and demoting the only admin are
		// rejected rather than silently applied.
		var admins int
		if err := a.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM users WHERE COALESCE(role,'admin')='admin'`).Scan(&admins); err != nil {
			fail(w, http.StatusInternalServerError, errors.New("could not read account roles"))
			return
		}
		if id == selfID || admins <= 1 {
			fail(w, http.StatusConflict, errors.New("at least one administrator must remain"))
			return
		}
		if _, err := a.db.ExecContext(r.Context(), `UPDATE users SET role=? WHERE id=?`, input.Role, id); err != nil {
			slog.Error("update user role", "error", err)
			fail(w, http.StatusInternalServerError, errors.New("could not update the account"))
			return
		}
	}
	if input.Password != "" {
		if len(input.Password) < 8 || len(input.Password) > 72 {
			fail(w, http.StatusBadRequest, errors.New("password must be 8-72 bytes"))
			return
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(input.Password), bcrypt.DefaultCost)
		if err != nil {
			fail(w, http.StatusInternalServerError, errors.New("could not update the account"))
			return
		}
		if _, err := a.db.ExecContext(r.Context(), `UPDATE users SET password_hash=? WHERE id=?`, hash, id); err != nil {
			slog.Error("update user password", "error", err)
			fail(w, http.StatusInternalServerError, errors.New("could not update the account"))
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *App) deleteUser(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		fail(w, http.StatusBadRequest, errors.New("invalid user id"))
		return
	}
	session := r.Context().Value(sessionContextKey{}).(sessionContext)
	var selfID int64
	if err := a.db.QueryRowContext(r.Context(), `SELECT id FROM users WHERE username=?`, session.username).Scan(&selfID); err != nil {
		fail(w, http.StatusInternalServerError, errors.New("could not resolve the current account"))
		return
	}
	if id == selfID {
		fail(w, http.StatusConflict, errors.New("you cannot delete your own account"))
		return
	}
	var admins int
	if err := a.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM users WHERE COALESCE(role,'admin')='admin'`).Scan(&admins); err != nil {
		fail(w, http.StatusInternalServerError, errors.New("could not read account roles"))
		return
	}
	var targetRole string
	if err := a.db.QueryRowContext(r.Context(), `SELECT COALESCE(role,'admin') FROM users WHERE id=?`, id).Scan(&targetRole); err != nil {
		fail(w, http.StatusNotFound, errors.New("account not found"))
		return
	}
	if targetRole == roleAdmin && admins <= 1 {
		fail(w, http.StatusConflict, errors.New("at least one administrator must remain"))
		return
	}
	if _, err := a.db.ExecContext(r.Context(), `DELETE FROM users WHERE id=?`, id); err != nil {
		slog.Error("delete user", "error", err)
		fail(w, http.StatusInternalServerError, errors.New("could not delete the account"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
