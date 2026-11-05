package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"
	"viya/internal/secretstore"
)

type ApplyState struct {
	AccessConfirmation *accessConfirmation `json:"access_confirmation,omitempty"`
	DesiredHash        string              `json:"desired_hash"`
	AppliedHash        string              `json:"applied_hash"`
	Pending            bool                `json:"pending"`
	PortConfirmation   *panelConfirmation  `json:"port_confirmation,omitempty"`
	Stage              string              `json:"stage"`
	Message            string              `json:"message"`
	Updated            string              `json:"updated"`
	Recovery           bool                `json:"recovery"`
}
type checkpoint struct {
	Hash    string        `json:"hash"`
	Created string        `json:"created"`
	Payload configPayload `json:"payload"`
}
type sealedCheckpoint struct {
	Nonce []byte `json:"nonce"`
	Data  []byte `json:"data"`
}

func (a *App) setApplyStage(stage, message string) {
	a.applyStateMu.Lock()
	defer a.applyStateMu.Unlock()
	a.applyState.Stage = stage
	a.applyState.Message = message
	a.applyState.Updated = time.Now().UTC().Format(time.RFC3339)
}
func (a *App) checkpointPath(name string) string {
	return filepath.Join(a.dataDir, "recovery", name+".json")
}
func (a *App) readCheckpoint(name string) (checkpoint, error) {
	var cp checkpoint
	raw, e := os.ReadFile(a.checkpointPath(name))
	if e != nil {
		return cp, e
	}
	var sealed sealedCheckpoint
	if e = json.Unmarshal(raw, &sealed); e != nil {
		return cp, e
	}
	raw, e = secretstore.Decrypt(a.masterKey, sealed.Nonce, sealed.Data, []byte("viya:checkpoint:v1"))
	if e != nil {
		return cp, e
	}
	e = json.Unmarshal(raw, &cp)
	return cp, e
}
func (a *App) saveCheckpoint(ctx context.Context, body []byte) error {
	if a.dataDir == "" || len(a.masterKey) != 32 {
		return nil
	}
	hash := sha256.Sum256(body)
	hashJSON := hex.EncodeToString(hash[:])
	old, e := a.readCheckpoint("current")
	sameHash := e == nil && old.Hash == hashJSON
	payload, e := a.captureConfiguration(ctx, nil)
	if e != nil {
		return e
	}
	// Redacted engine revisions remain an independent audit trail.
	filtered := payload.Tables[:0]
	for _, table := range payload.Tables {
		if table.Name != "config_revisions" {
			filtered = append(filtered, table)
		}
	}
	payload.Tables = filtered
	cp := checkpoint{Hash: hashJSON, Created: time.Now().UTC().Format(time.RFC3339), Payload: payload}
	raw, e := json.Marshal(cp)
	if e != nil {
		return e
	}
	nonce, data, e := secretstore.Encrypt(a.masterKey, raw, []byte("viya:checkpoint:v1"))
	if e != nil {
		return e
	}
	raw, e = json.Marshal(sealedCheckpoint{nonce, data})
	if e != nil {
		return e
	}
	dir := filepath.Dir(a.checkpointPath("current"))
	if e = os.MkdirAll(dir, 0700); e != nil {
		return e
	}
	f, e := os.CreateTemp(dir, ".checkpoint-*")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	if e = f.Chmod(0600); e != nil {
		f.Close()
		return e
	}
	if _, e = f.Write(raw); e != nil {
		f.Close()
		return e
	}
	if e = f.Sync(); e != nil {
		f.Close()
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	if !sameHash && old.Hash != "" {
		previous, e := os.ReadFile(a.checkpointPath("current"))
		if e != nil {
			return e
		}
		if e = atomicRecoveryFile(a.checkpointPath("previous"), previous); e != nil {
			return e
		}
	}
	if e = os.Rename(f.Name(), a.checkpointPath("current")); e != nil {
		return e
	}
	return a.archiveDailyCheckpoint()
}
func (a *App) configurationStatus(w http.ResponseWriter, r *http.Request) {
	a.applyStateMu.RLock()
	state := a.applyState
	a.applyStateMu.RUnlock()
	name := "previous"
	if state.Stage == "failed" || state.Stage == "applied-warning" {
		name = "current"
	}
	_, e := os.Stat(a.checkpointPath(name))
	state.Recovery = e == nil
	state.PortConfirmation = a.pendingPanelConfirmation(r.Context())
	state.AccessConfirmation = a.pendingAccessConfirmation(r.Context())
	writeJSON(w, 200, state)
}
func (a *App) rollbackConfiguration(w http.ResponseWriter, r *http.Request) {
	if a.pendingAccessConfirmation(r.Context()) != nil {
		fail(w, 409, errors.New("请先确认或取消待生效的面板访问限制"))
		return
	}
	var input struct {
		Confirmed  bool   `json:"confirmed"`
		Checkpoint string `json:"checkpoint"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&input) != nil || !input.Confirmed {
		fail(w, 400, errors.New("请确认恢复最近可用配置"))
		return
	}
	a.listenerMu.Lock()
	defer a.listenerMu.Unlock()
	a.ddnsMu.Lock()
	defer a.ddnsMu.Unlock()
	a.mu.Lock()
	defer a.mu.Unlock()
	a.applyStateMu.RLock()
	stage := a.applyState.Stage
	a.applyStateMu.RUnlock()
	name := "previous"
	if stage == "failed" || stage == "applied-warning" {
		name = "current"
	}
	if input.Checkpoint != "" {
		if !automaticBackupName.MatchString(input.Checkpoint) {
			fail(w, 400, errors.New("无效的恢复点名称"))
			return
		}
		name = input.Checkpoint
	}
	cp, e := a.readCheckpoint(name)
	if e != nil {
		fail(w, 400, errors.New("没有可用的配置恢复点"))
		return
	}
	runtime, e := captureTables(r.Context(), a.db, runtimeTables)
	if e != nil {
		fail(w, 500, e)
		return
	}
	before, e := a.captureConfiguration(r.Context(), nil)
	if e != nil {
		fail(w, 500, e)
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 90*time.Second)
	defer cancel()
	restore := func(payload configPayload) error {
		tx, e := a.db.BeginTx(ctx, nil)
		if e != nil {
			return e
		}
		defer tx.Rollback()
		if e = restoreTables(ctx, tx, payload.Tables); e != nil {
			return e
		}
		if e = restoreRecoveryRuntime(ctx, tx, runtime); e != nil {
			return e
		}
		if e = tx.Commit(); e != nil {
			return e
		}
		for _, f := range payload.Files {
			if !allowedConfigFile(f.Path) {
				return errors.New("恢复文件无效")
			}
			dest := filepath.Join(a.dataDir, filepath.FromSlash(f.Path))
			if e = os.MkdirAll(filepath.Dir(dest), 0700); e != nil {
				return e
			}
			if e = os.WriteFile(dest, f.Data, 0600); e != nil {
				return e
			}
		}
		return nil
	}
	if e = restore(cp.Payload); e == nil {
		e = a.validateRestoredBindings(ctx)
	}
	if e == nil {
		e = a.applyLocked(ctx)
	}
	if e != nil {
		restoreErr := restore(before)
		engineErr := a.applyLocked(ctx)
		if restoreErr != nil || engineErr != nil {
			fail(w, 500, errors.New("恢复失败，请使用服务器备份恢复"))
			return
		}
		fail(w, 400, errors.New("恢复点校验失败，已还原本次恢复前的配置"))
		return
	}
	a.configureProgramLogging()
	writeJSON(w, 200, map[string]any{"ok": true, "created": cp.Created, "message": "已恢复可用配置；账号保持不变，监听端口设置在重启后生效"})
}

// Preserve samples and logs for objects that still exist after recovery.
func restoreRecoveryRuntime(ctx context.Context, tx *sql.Tx, tables []tableDump) error {
	refs := map[string][2]string{"traffic_hours": {"site_id", "proxy_hosts"}, "site_access_events": {"site_id", "proxy_hosts"}, "ddns_runs": {"ddns_record_id", "ddns_records"}}
	filtered := make([]tableDump, 0, len(tables))
	for _, table := range tables {
		ref, has := refs[table.Name]
		if has {
			valid := map[string]bool{}
			rows, e := tx.QueryContext(ctx, "SELECT id FROM "+quoteColumn(ref[1]))
			if e != nil {
				return e
			}
			for rows.Next() {
				var id int64
				if e = rows.Scan(&id); e != nil {
					rows.Close()
					return e
				}
				valid[strconv.FormatInt(id, 10)] = true
			}
			e = rows.Err()
			rows.Close()
			if e != nil {
				return e
			}
			column := -1
			for i, name := range table.Columns {
				if name == ref[0] {
					column = i
					break
				}
			}
			if column < 0 {
				return errors.New("运行时引用无效")
			}
			kept := [][]configCell{}
			for _, row := range table.Rows {
				if valid[row[column].Value] {
					kept = append(kept, row)
				}
			}
			table.Rows = kept
		}
		filtered = append(filtered, table)
	}
	return restoreTables(ctx, tx, filtered)
}

func atomicRecoveryFile(path string, data []byte) error {
	f, e := os.CreateTemp(filepath.Dir(path), ".recovery-*")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	if e = f.Chmod(0600); e != nil {
		f.Close()
		return e
	}
	if _, e = f.Write(data); e != nil {
		f.Close()
		return e
	}
	if e = f.Sync(); e != nil {
		f.Close()
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	return os.Rename(f.Name(), path)
}

// Metadata and DDNS settings do not change the compiled engine configuration.
// Refresh their encrypted backup only while the desired engine config still
// matches the working checkpoint, so a failed pending edit cannot replace it.
func (a *App) refreshWorkingCheckpoint(ctx context.Context) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.applyStateMu.RLock()
	stage := a.applyState.Stage
	a.applyStateMu.RUnlock()
	if stage != "applied" {
		return
	}
	current, e := a.readCheckpoint("current")
	if e != nil {
		return
	}
	body, e := a.compileConfig(ctx)
	if e != nil {
		return
	}
	hash := sha256.Sum256(body)
	if current.Hash != hex.EncodeToString(hash[:]) {
		return
	}
	if e = a.saveCheckpoint(ctx, body); e != nil {
		a.setApplyStage("applied-warning", "配置已生效，但恢复点保存失败")
	}
}
