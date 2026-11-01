package main

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"time"
)

type panelConfirmation struct {
	OldPort  string `json:"old_port"`
	NewPort  string `json:"new_port"`
	Deadline int64  `json:"deadline"`
}

func (a *App) pendingPanelConfirmation(ctx context.Context) *panelConfirmation {
	var raw string
	if a.db.QueryRowContext(ctx, `SELECT value FROM panel_settings WHERE key='pending_panel_port'`).Scan(&raw) != nil {
		return nil
	}
	var p panelConfirmation
	if json.Unmarshal([]byte(raw), &p) != nil {
		return nil
	}
	return &p
}
func (a *App) restoreExpiredPanelPort(ctx context.Context) (bool, error) {
	p := a.pendingPanelConfirmation(ctx)
	if p == nil || time.Now().Unix() < p.Deadline {
		return false, nil
	}
	tx, e := a.db.BeginTx(ctx, nil)
	if e != nil {
		return false, e
	}
	defer tx.Rollback()
	if _, e = tx.ExecContext(ctx, `INSERT INTO panel_settings(key,value) VALUES('listen_port',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, p.OldPort); e != nil {
		return false, e
	}
	if _, e = tx.ExecContext(ctx, `DELETE FROM panel_settings WHERE key='pending_panel_port'`); e != nil {
		return false, e
	}
	return true, tx.Commit()
}
func (a *App) watchPanelConfirmation() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-a.ctx.Done():
			return
		case <-ticker.C:
			a.configMu.Lock()
			restored, e := a.restoreExpiredPanelPort(a.ctx)
			a.configMu.Unlock()
			if e == nil && restored {
				if process, e := os.FindProcess(os.Getpid()); e == nil {
					_ = process.Signal(os.Interrupt)
				}
				return
			}
		}
	}
}
func (a *App) confirmPanelAccess(w http.ResponseWriter, r *http.Request) {
	p := a.pendingPanelConfirmation(r.Context())
	if p == nil {
		w.WriteHeader(204)
		return
	}
	_, port, e := net.SplitHostPort(a.panelListen)
	if e != nil || port != p.NewPort {
		fail(w, 409, errors.New("请从新端口连接后确认"))
		return
	}
	if time.Now().Unix() >= p.Deadline {
		fail(w, 409, errors.New("确认已超时，旧端口即将恢复"))
		return
	}
	if _, e = a.db.ExecContext(r.Context(), `DELETE FROM panel_settings WHERE key='pending_panel_port'`); e != nil {
		fail(w, 500, e)
		return
	}
	w.WriteHeader(204)
}
