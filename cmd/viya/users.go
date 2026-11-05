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
		fail(w, http.StatusInternalServerError, errors.New("无法获取用户列表"))
		return
	}
	defer rows.Close()
	out := []panelUser{}
	for rows.Next() {
		var user panelUser
		if err := rows.Scan(&user.ID, &user.Username, &user.Role, &user.CreatedAt); err != nil {
			fail(w, http.StatusInternalServerError, errors.New("无法获取用户列表"))
			return
		}
		out = append(out, user)
	}
	if err := rows.Err(); err != nil {
		fail(w, http.StatusInternalServerError, errors.New("无法获取用户列表"))
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
		fail(w, http.StatusBadRequest, errors.New("用户名需为 3-64 个字符，只能包含字母、数字、点、连字符和下划线"))
		return
	}
	if len(input.Password) < 8 || len(input.Password) > 72 {
		fail(w, http.StatusBadRequest, errors.New("密码需为 8-72 字节"))
		return
	}
	if input.Role != roleViewer && input.Role != roleAdmin {
		fail(w, http.StatusBadRequest, errors.New("角色只能是管理员或只读"))
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(input.Password), bcrypt.DefaultCost)
	if err != nil {
		fail(w, http.StatusInternalServerError, errors.New("无法创建账号"))
		return
	}
	var existing int
	if err := a.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM users WHERE username=?`, username).Scan(&existing); err == nil && existing > 0 {
		fail(w, http.StatusConflict, errors.New("该用户名已被占用"))
		return
	}
	result, err := a.db.ExecContext(r.Context(), `INSERT INTO users(username,password_hash,role) VALUES(?,?,?)`, username, hash, input.Role)
	if err != nil {
		fail(w, http.StatusConflict, errors.New("无法创建账号"))
		return
	}
	id, _ := result.LastInsertId()
	writeJSON(w, http.StatusCreated, panelUser{ID: id, Username: username, Role: input.Role})
}

func (a *App) updateUser(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		fail(w, http.StatusBadRequest, errors.New("用户 ID 无效"))
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
		fail(w, http.StatusInternalServerError, errors.New("无法识别当前账号"))
		return
	}
	if input.Role != "" {
		if input.Role != roleViewer && input.Role != roleAdmin {
			fail(w, http.StatusBadRequest, errors.New("角色只能是管理员或只读"))
			return
		}
		// Losing the last administrator would leave nobody able to manage the
		// panel, so both demoting yourself and demoting the only admin are
		// rejected rather than silently applied.
		var admins int
		if err := a.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM users WHERE COALESCE(role,'admin')='admin'`).Scan(&admins); err != nil {
			fail(w, http.StatusInternalServerError, errors.New("无法读取账号角色"))
			return
		}
		if id == selfID || admins <= 1 {
			fail(w, http.StatusConflict, errors.New("必须保留至少一个管理员"))
			return
		}
	}
	if input.Role == "" && input.Password == "" {
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
		return
	}
	tx, err := a.db.BeginTx(r.Context(), nil)
	if err != nil {
		fail(w, http.StatusInternalServerError, errors.New("无法更新账号"))
		return
	}
	defer tx.Rollback()
	if input.Role != "" {
		if _, err := tx.ExecContext(r.Context(), `UPDATE users SET role=? WHERE id=?`, input.Role, id); err != nil {
			slog.Error("update user role", "error", err)
			fail(w, http.StatusInternalServerError, errors.New("无法更新账号"))
			return
		}
	}
	if input.Password != "" {
		if len(input.Password) < 8 || len(input.Password) > 72 {
			fail(w, http.StatusBadRequest, errors.New("密码需为 8-72 字节"))
			return
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(input.Password), bcrypt.DefaultCost)
		if err != nil {
			fail(w, http.StatusInternalServerError, errors.New("无法更新账号"))
			return
		}
		if _, err := tx.ExecContext(r.Context(), `UPDATE users SET password_hash=? WHERE id=?`, hash, id); err != nil {
			slog.Error("update user password", "error", err)
			fail(w, http.StatusInternalServerError, errors.New("无法更新账号"))
			return
		}
		// A password reset by an administrator must invalidate the target
		// account's existing sessions; do not rely on foreign-key cascades.
		if _, err := tx.ExecContext(r.Context(), `DELETE FROM sessions WHERE user_id=?`, id); err != nil {
			slog.Error("invalidate sessions on password reset", "error", err)
			fail(w, http.StatusInternalServerError, errors.New("无法更新账号"))
			return
		}
	}
	if err := tx.Commit(); err != nil {
		fail(w, http.StatusInternalServerError, errors.New("无法更新账号"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *App) deleteUser(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		fail(w, http.StatusBadRequest, errors.New("用户 ID 无效"))
		return
	}
	session := r.Context().Value(sessionContextKey{}).(sessionContext)
	var selfID int64
	if err := a.db.QueryRowContext(r.Context(), `SELECT id FROM users WHERE username=?`, session.username).Scan(&selfID); err != nil {
		fail(w, http.StatusInternalServerError, errors.New("无法识别当前账号"))
		return
	}
	if id == selfID {
		fail(w, http.StatusConflict, errors.New("不能删除当前登录的账号"))
		return
	}
	var admins int
	if err := a.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM users WHERE COALESCE(role,'admin')='admin'`).Scan(&admins); err != nil {
		fail(w, http.StatusInternalServerError, errors.New("无法读取账号角色"))
		return
	}
	var targetRole string
	if err := a.db.QueryRowContext(r.Context(), `SELECT COALESCE(role,'admin') FROM users WHERE id=?`, id).Scan(&targetRole); err != nil {
		fail(w, http.StatusNotFound, errors.New("账号不存在"))
		return
	}
	if targetRole == roleAdmin && admins <= 1 {
		fail(w, http.StatusConflict, errors.New("必须保留至少一个管理员"))
		return
	}
	tx, err := a.db.BeginTx(r.Context(), nil)
	if err != nil {
		fail(w, http.StatusInternalServerError, errors.New("无法删除账号"))
		return
	}
	defer tx.Rollback()
	// Explicitly revoke the account's sessions; foreign-key cascades are a
	// connection-level PRAGMA and must not be relied upon.
	if _, err := tx.ExecContext(r.Context(), `DELETE FROM sessions WHERE user_id=?`, id); err != nil {
		slog.Error("delete user sessions", "error", err)
		fail(w, http.StatusInternalServerError, errors.New("无法删除账号"))
		return
	}
	if _, err := tx.ExecContext(r.Context(), `DELETE FROM users WHERE id=?`, id); err != nil {
		slog.Error("delete user", "error", err)
		fail(w, http.StatusInternalServerError, errors.New("无法删除账号"))
		return
	}
	if err := tx.Commit(); err != nil {
		fail(w, http.StatusInternalServerError, errors.New("无法删除账号"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
