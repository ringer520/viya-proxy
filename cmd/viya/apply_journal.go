package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"
)

func failSavedApplication(w http.ResponseWriter) {
	writeJSON(w, 502, map[string]any{"error": "配置已保存，但尚未生效。请重试应用或在配置历史中恢复。", "saved": true, "applied": false, "code": "configuration_pending"})
}

func configurationHash(body []byte) string {
	var value any
	if json.Unmarshal(body, &value) == nil {
		body, _ = json.Marshal(value)
	}
	hash := sha256.Sum256(body)
	return hex.EncodeToString(hash[:])
}

func (a *App) journalApplication(body []byte, applied bool) {
	hash := configurationHash(body)
	a.applyStateMu.Lock()
	a.applyState.DesiredHash = hash
	if applied {
		a.applyState.AppliedHash = hash
	}
	a.applyState.Pending = a.applyState.DesiredHash != a.applyState.AppliedHash
	state := a.applyState
	a.applyStateMu.Unlock()
	// A browser disconnect must not erase the result of an engine operation.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := a.db.ExecContext(ctx, `INSERT INTO config_application(id,desired_hash,applied_hash,stage,updated) VALUES(1,?,?,?,?) ON CONFLICT(id) DO UPDATE SET desired_hash=excluded.desired_hash,applied_hash=excluded.applied_hash,stage=excluded.stage,updated=excluded.updated`, state.DesiredHash, state.AppliedHash, state.Stage, time.Now().UTC().Format(time.RFC3339)); err != nil {
		slog.Warn("persist configuration application identity", "error", err)
	}
}

func (a *App) loadApplicationJournal() {
	_ = a.db.QueryRow(`SELECT desired_hash,applied_hash,stage,updated FROM config_application WHERE id=1`).Scan(&a.applyState.DesiredHash, &a.applyState.AppliedHash, &a.applyState.Stage, &a.applyState.Updated)
	a.applyState.Pending = a.applyState.DesiredHash != a.applyState.AppliedHash
}

// A handler may restore its database changes after the shared apply pipeline
// restored the engine. Clear pending status only when both actually agree.
func (a *App) confirmRestoredApplication(ctx context.Context, running []byte) {
	desired, err := a.compileConfig(ctx)
	if err != nil {
		return
	}
	if running == nil {
		running, err = a.livePolicyConfig(ctx)
		if err != nil {
			return
		}
	}
	if configurationHash(desired) != configurationHash(running) {
		return
	}
	a.setApplyStage("applied", "配置已恢复")
	a.journalApplication(desired, true)
}
