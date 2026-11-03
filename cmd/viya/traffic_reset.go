package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"
)

func (a *App) resetTraffic(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Confirmed bool `json:"confirmed"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&input) != nil || !input.Confirmed {
		fail(w, 400, errors.New("请确认清空所有流量统计"))
		return
	}
	a.trafficMu.Lock()
	defer a.trafficMu.Unlock()
	tx, err := a.db.BeginTx(r.Context(), nil)
	if err != nil {
		fail(w, 500, err)
		return
	}
	defer tx.Rollback()
	at := strconv.FormatFloat(float64(time.Now().UnixMicro())/1e6, 'f', 6, 64)
	for _, table := range []string{"traffic_hours", "layer4_traffic_hours", "site_access_events"} {
		if _, err = tx.ExecContext(r.Context(), "DELETE FROM "+table); err != nil {
			fail(w, 500, err)
			return
		}
	}
	_, err = tx.ExecContext(r.Context(), `INSERT INTO panel_settings(key,value) VALUES('traffic_reset_at',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, at)
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		fail(w, 500, err)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}
