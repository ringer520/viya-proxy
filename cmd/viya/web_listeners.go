package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type WebListener struct {
	ID       string `json:"id"`
	CopyFrom string `json:"copy_from,omitempty"`
	Protocol string `json:"protocol"`
	Port     int    `json:"port"`
}

func (a *App) webListeners(ctx context.Context) ([]WebListener, error) {
	var raw string
	err := a.db.QueryRowContext(ctx, `SELECT value FROM panel_settings WHERE key='web_listeners'`).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return []WebListener{{ID: "https-main", Protocol: "https", Port: defaultListenerPort("VIYA_DEFAULT_HTTPS_PORT", 443)}, {ID: "http-main", Protocol: "http", Port: defaultListenerPort("VIYA_DEFAULT_HTTP_PORT", 80)}}, nil
	}
	if err != nil {
		return nil, err
	}
	var result []WebListener
	if err = json.Unmarshal([]byte(raw), &result); err != nil {
		return nil, err
	}
	for i := range result {
		if result[i].ID == "" {
			result[i].ID = fmt.Sprintf("%s-%d", result[i].Protocol, result[i].Port)
		}
	}
	return result, validateWebListeners(result)
}
func validateWebListeners(listeners []WebListener) error {
	if len(listeners) < 2 || len(listeners) > 20 {
		return errors.New("监听数量必须在 2–20 之间")
	}
	ids := map[string]bool{}
	ports := map[int]bool{}
	protocols := map[string]bool{}
	for _, listener := range listeners {
		if len(listener.ID) < 1 || len(listener.ID) > 80 || ids[listener.ID] {
			return errors.New("监听标识无效或重复")
		}
		for _, c := range listener.ID {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
				return errors.New("监听标识仅支持字母、数字、横线和下划线")
			}
		}
		ids[listener.ID] = true
		if (listener.Protocol != "http" && listener.Protocol != "https") || listener.Port < 1 || listener.Port > 65535 || ports[listener.Port] {
			return errors.New("监听协议、端口范围无效或端口重复")
		}
		ports[listener.Port] = true
		protocols[listener.Protocol] = true
	}
	if !protocols["http"] || !protocols["https"] {
		return errors.New("至少保留一个 HTTP 和 HTTPS 监听")
	}
	return nil
}
func (a *App) getWebListeners(w http.ResponseWriter, r *http.Request) {
	listeners, err := a.webListeners(r.Context())
	if err != nil {
		fail(w, 500, err)
		return
	}
	writeJSON(w, 200, listeners)
}

// A bound port is reusable only when the running Caddy config owns it.
func (a *App) listenerPortAvailable(ctx context.Context, port int, protocol string) error {
	if port < 1 || port > 65535 {
		return errors.New("端口范围为 1–65535")
	}
	for _, address := range []string{a.adminListen, env("VIYA_LISTEN", "127.0.0.1:8181")} {
		_, p, _ := net.SplitHostPort(address)
		if p == strconv.Itoa(port) {
			return errors.New("此端口被管理接口占用")
		}
	}
	var panelPort int
	_ = a.db.QueryRowContext(ctx, `SELECT CAST(value AS INTEGER) FROM panel_settings WHERE key='listen_port'`).Scan(&panelPort)
	if port == panelPort {
		return errors.New("此端口被面板占用")
	}
	var count int
	if err := a.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM layer4_rules WHERE enabled=1 AND listen_port=? AND protocol IN ('tcp','both')`, port).Scan(&count); err != nil {
		return err
	}
	if protocol == "https" && count == 0 {
		if err := a.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM layer4_rules WHERE enabled=1 AND listen_port=? AND protocol IN ('udp','both')`, port).Scan(&count); err != nil {
			return err
		}
	}
	if count > 0 {
		return errors.New("此端口被 TCP / UDP 转发占用")
	}
	ln, tcpErr := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if tcpErr == nil {
		ln.Close()
	}
	var udpErr error
	if protocol == "https" {
		packet, e := net.ListenPacket("udp", fmt.Sprintf(":%d", port))
		udpErr = e
		if e == nil {
			packet.Close()
		}
	}
	if tcpErr == nil && udpErr == nil {
		return nil
	}
	req, e := http.NewRequestWithContext(ctx, "GET", strings.TrimRight(a.adminURL, "/")+"/config/apps/http/servers", nil)
	if e == nil {
		response, e := (&http.Client{Timeout: 2 * time.Second}).Do(req)
		if e == nil {
			defer response.Body.Close()
			var servers map[string]struct {
				Listen []string          `json:"listen"`
				TLS    []json.RawMessage `json:"tls_connection_policies"`
			}
			if response.StatusCode == 200 && json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&servers) == nil {
				for name, server := range servers {
					if name != "viya" && !strings.HasPrefix(name, "viya_") {
						continue
					}
					for _, address := range server.Listen {
						_, p, _ := net.SplitHostPort(address)
						if p == strconv.Itoa(port) && (udpErr == nil || len(server.TLS) > 0 || port == 443) {
							return nil
						}
					}
				}
			}
		}
	}
	return errors.New("端口已被占用或当前服务没有监听权限")
}
func (a *App) checkWebListener(w http.ResponseWriter, r *http.Request) {
	port, err := strconv.Atoi(r.URL.Query().Get("port"))
	if err == nil {
		protocol := r.URL.Query().Get("protocol")
		if protocol != "http" && protocol != "https" {
			fail(w, 400, errors.New("协议只能是 HTTP 或 HTTPS"))
			return
		}
		err = a.listenerPortAvailable(r.Context(), port, protocol)
	}
	message := "端口可用"
	if err != nil {
		message = err.Error()
	}
	writeJSON(w, 200, map[string]any{"available": err == nil, "message": message})
}
func (a *App) saveWebListeners(w http.ResponseWriter, r *http.Request) {
	a.listenerMu.Lock()
	defer a.listenerMu.Unlock()
	var listeners []WebListener
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&listeners) != nil || len(listeners) < 2 || len(listeners) > 20 {
		fail(w, 400, errors.New("请保留 HTTP 和 HTTPS 监听，最多 20 个"))
		return
	}
	if err := validateWebListeners(listeners); err != nil {
		fail(w, 400, err)
		return
	}
	for _, listener := range listeners {
		if err := a.listenerPortAvailable(r.Context(), listener.Port, listener.Protocol); err != nil {
			fail(w, 409, fmt.Errorf("端口 %d：%w", listener.Port, err))
			return
		}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	old, err := a.webListeners(r.Context())
	if err != nil {
		fail(w, 500, err)
		return
	}
	snapshot, err := captureTables(r.Context(), a.db, []string{"proxy_hosts", "traffic_hours", "site_access_events"})
	if err != nil {
		fail(w, 500, err)
		return
	}
	oldIDs := map[string]bool{}
	for _, v := range old {
		oldIDs[v.ID] = true
	}
	tx, err := a.db.BeginTx(r.Context(), nil)
	if err != nil {
		fail(w, 500, err)
		return
	}
	defer tx.Rollback()
	keep := map[string]bool{}
	for i := range listeners {
		v := &listeners[i]
		keep[v.ID] = true
		if !oldIDs[v.ID] && v.CopyFrom != "" {
			if !oldIDs[v.CopyFrom] {
				fail(w, 400, errors.New("导入来源监听不存在"))
				return
			}
			if err = cloneListenerSites(r.Context(), tx, v.CopyFrom, v.ID, v.Protocol); err != nil {
				fail(w, 500, err)
				return
			}
		}
		v.CopyFrom = ""
	}
	// Deleted listeners remove their owned sites only after the UI's explicit confirmation.
	for _, v := range old {
		if !keep[v.ID] {
			if _, err = tx.ExecContext(r.Context(), `DELETE FROM proxy_hosts WHERE listener_id=?`, v.ID); err != nil {
				fail(w, 500, err)
				return
			}
		}
	}
	raw, _ := json.Marshal(listeners)
	if _, err = tx.ExecContext(r.Context(), `INSERT INTO panel_settings(key,value) VALUES('web_listeners',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, string(raw)); err != nil {
		fail(w, 500, err)
		return
	}
	if err = tx.Commit(); err != nil {
		fail(w, 500, err)
		return
	}
	if err = a.applyLocked(r.Context()); err != nil {
		previous, _ := json.Marshal(old)
		recoveryCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		recoveryTx, rollbackErr := a.db.BeginTx(recoveryCtx, nil)
		if rollbackErr == nil {
			rollbackErr = restoreTables(recoveryCtx, recoveryTx, snapshot)
			if rollbackErr == nil {
				_, rollbackErr = recoveryTx.ExecContext(recoveryCtx, `UPDATE panel_settings SET value=? WHERE key='web_listeners'`, string(previous))
			}
			if rollbackErr == nil {
				rollbackErr = recoveryTx.Commit()
			} else {
				recoveryTx.Rollback()
			}
		}
		if rollbackErr != nil {
			fail(w, 500, errors.New("监听应用失败且无法恢复设置，请检查配置历史"))
			return
		}
		if recoveryErr := a.applyLocked(recoveryCtx); recoveryErr != nil {
			fail(w, 502, errors.New("监听保存失败，原设置已恢复但引擎尚未应用，请重新应用配置"))
			return
		}
		fail(w, 502, errors.New("监听应用失败，已恢复原设置，请检查端口权限及配置历史"))
		return
	}
	writeJSON(w, 200, listeners)
}

func defaultListenerPort(key string, fallback int) int {
	value, err := strconv.Atoi(env(key, strconv.Itoa(fallback)))
	if err != nil || value < 1 || value > 65535 {
		return fallback
	}
	return value
}
