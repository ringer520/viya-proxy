package main

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func (a *App) getPanelSettings(w http.ResponseWriter, r *http.Request) {
	addr := env("VIYA_LISTEN", "127.0.0.1:8181")
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		fail(w, http.StatusInternalServerError, errors.New("面板监听地址无效"))
		return
	}
	var stored string
	if err = a.db.QueryRowContext(r.Context(), `SELECT value FROM panel_settings WHERE key='listen_port'`).Scan(&stored); err == nil {
		port = stored
	}
	session := r.Context().Value(sessionContextKey{}).(sessionContext)
	var active int
	if err := a.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM sessions JOIN users ON users.id=sessions.user_id WHERE users.username=? AND sessions.expires_at>?`, session.username, time.Now().Unix()).Scan(&active); err != nil {
		fail(w, 500, errors.New("无法读取登录会话"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"listen_host": host, "listen_port": port, "username": session.username, "active_sessions": active})
}

func (a *App) updatePanelPort(w http.ResponseWriter, r *http.Request) {
	a.listenerMu.Lock()
	defer a.listenerMu.Unlock()
	var input struct {
		Port int `json:"port"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&input); err != nil || input.Port < 1024 || input.Port > 65535 {
		fail(w, http.StatusBadRequest, errors.New("面板端口必须在 1024 到 65535 之间"))
		return
	}
	addr := env("VIYA_LISTEN", "127.0.0.1:8181")
	host, current, err := net.SplitHostPort(addr)
	if err != nil {
		fail(w, http.StatusInternalServerError, errors.New("面板监听地址无效"))
		return
	}
	var saved string
	if a.db.QueryRowContext(r.Context(), `SELECT value FROM panel_settings WHERE key='listen_port'`).Scan(&saved) == nil {
		current = saved
	}
	if current == itoa(input.Port) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "unchanged": true, "new_port": current})
		return
	}
	probe, err := net.Listen("tcp", net.JoinHostPort(host, itoa(input.Port)))
	if err != nil {
		fail(w, http.StatusConflict, errors.New("该端口无法监听，请选择未占用端口"))
		return
	}
	_ = probe.Close()

	if a.pendingPanelConfirmation(r.Context()) != nil {
		fail(w, 409, errors.New("请先确认当前端口变更或等待自动恢复"))
		return
	}
	pending, _ := json.Marshal(panelConfirmation{OldPort: current, NewPort: itoa(input.Port), Deadline: time.Now().Add(90 * time.Second).Unix()})
	tx, err := a.db.BeginTx(r.Context(), nil)
	if err != nil {
		fail(w, 500, err)
		return
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(r.Context(), `INSERT INTO panel_settings(key,value) VALUES('pending_panel_port',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, string(pending)); err != nil {
		fail(w, 500, err)
		return
	}
	if _, err = tx.ExecContext(r.Context(), `INSERT INTO panel_settings(key,value) VALUES('listen_port',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, itoa(input.Port)); err == nil {
		err = tx.Commit()
	}
	if err != nil {
		fail(w, http.StatusInternalServerError, errors.New("保存面板端口失败"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "old_port": current, "new_port": itoa(input.Port), "restart_in_seconds": 2})
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
	time.AfterFunc(2*time.Second, func() {
		if process, err := os.FindProcess(os.Getpid()); err == nil {
			_ = process.Signal(os.Interrupt)
		}
	})
}

func (a *App) changeAdminPassword(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Current string `json:"current_password"`
		New     string `json:"new_password"`
		Confirm string `json:"confirm_password"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&input); err != nil {
		fail(w, http.StatusBadRequest, errors.New("密码请求格式无效"))
		return
	}
	if len(input.New) < 8 || len(input.New) > 72 {
		fail(w, http.StatusBadRequest, errors.New("新密码长度须为 8 到 72 字节"))
		return
	}
	if input.New != input.Confirm {
		fail(w, http.StatusBadRequest, errors.New("两次输入的新密码不一致"))
		return
	}
	session := r.Context().Value(sessionContextKey{}).(sessionContext)
	var id int64
	var hash []byte
	if err := a.db.QueryRowContext(r.Context(), `SELECT id,password_hash FROM users WHERE username=?`, session.username).Scan(&id, &hash); err != nil || bcrypt.CompareHashAndPassword(hash, []byte(input.Current)) != nil {
		fail(w, http.StatusUnauthorized, errors.New("当前密码不正确"))
		return
	}
	newHash, err := bcrypt.GenerateFromPassword([]byte(input.New), bcrypt.DefaultCost)
	if err != nil {
		fail(w, http.StatusInternalServerError, errors.New("密码更新失败"))
		return
	}
	secret, _ := sessionCookie(r)
	tokenHash := sha256.Sum256([]byte(secret))
	tx, err := a.db.BeginTx(r.Context(), nil)
	if err != nil {
		fail(w, http.StatusInternalServerError, errors.New("密码更新失败"))
		return
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(r.Context(), `UPDATE users SET password_hash=? WHERE id=?`, newHash, id); err == nil {
		_, err = tx.ExecContext(r.Context(), `DELETE FROM sessions WHERE user_id=? AND token_hash<>?`, id, tokenHash[:])
	}
	if err != nil || tx.Commit() != nil {
		fail(w, http.StatusInternalServerError, errors.New("密码更新失败"))
		return
	}
	a.recordLoginAudit(r, session.username, auditSuccess, auditReasonPasswordEdit)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func itoa(value int) string {
	return strconv.Itoa(value)
}

// Preserve the current session while revoking other sessions for this administrator.
func (a *App) revokeOtherSessions(w http.ResponseWriter, r *http.Request) {
	session := r.Context().Value(sessionContextKey{}).(sessionContext)
	secret, ok := sessionCookie(r)
	if !ok {
		fail(w, 401, errors.New("需要登录"))
		return
	}
	hash := sha256.Sum256([]byte(secret))
	result, err := a.db.ExecContext(r.Context(), `DELETE FROM sessions WHERE user_id=(SELECT id FROM users WHERE username=?) AND token_hash<>? AND expires_at>?`, session.username, hash[:], time.Now().Unix())
	if err != nil {
		fail(w, 500, errors.New("无法退出其他登录会话"))
		return
	}
	count, err := result.RowsAffected()
	if err != nil {
		fail(w, 500, errors.New("无法读取会话处理结果"))
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "revoked": count})
	a.recordLoginAudit(r, session.username, auditSuccess, auditReasonSessionOff)
}

func (a *App) changeUsername(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&input) != nil {
		fail(w, 400, errors.New("账号格式无效"))
		return
	}
	input.Username = strings.TrimSpace(input.Username)
	if len(input.Username) < 3 || len(input.Username) > 64 {
		fail(w, 400, errors.New("账号长度须为 3–64 位"))
		return
	}
	for _, c := range input.Username {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-' || c == '.') {
			fail(w, 400, errors.New("账号仅支持字母、数字、点、横线和下划线"))
			return
		}
	}
	session := r.Context().Value(sessionContextKey{}).(sessionContext)
	var id int64
	var hash []byte
	if a.db.QueryRowContext(r.Context(), `SELECT id,password_hash FROM users WHERE username=?`, session.username).Scan(&id, &hash) != nil || bcrypt.CompareHashAndPassword(hash, []byte(input.Password)) != nil {
		fail(w, 400, errors.New("当前密码不正确"))
		return
	}
	tx, e := a.db.BeginTx(r.Context(), nil)
	if e != nil {
		fail(w, 500, e)
		return
	}
	defer tx.Rollback()
	if _, e = tx.ExecContext(r.Context(), `UPDATE users SET username=? WHERE id=?`, input.Username, id); e != nil {
		fail(w, 409, errors.New("账号已存在或无法修改"))
		return
	}
	secret, _ := sessionCookie(r)
	token := sha256.Sum256([]byte(secret))
	if _, e = tx.ExecContext(r.Context(), `DELETE FROM sessions WHERE user_id=? AND token_hash<>?`, id, token[:]); e != nil {
		fail(w, 500, e)
		return
	}
	if e = tx.Commit(); e != nil {
		fail(w, 500, e)
		return
	}
	writeJSON(w, 200, map[string]string{"username": input.Username})
}
