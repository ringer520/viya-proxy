package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func (a *App) listLayer4Rules(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.QueryContext(r.Context(), `SELECT id,name,listen_address,listen_port,upstream,protocol,enabled,version FROM layer4_rules ORDER BY listen_port,protocol`)
	if err != nil {
		fail(w, 500, errors.New("无法读取端口转发规则"))
		return
	}
	defer rows.Close()
	out := []Layer4Rule{}
	for rows.Next() {
		var v Layer4Rule
		var enabled int
		if err := rows.Scan(&v.ID, &v.Name, &v.ListenAddress, &v.ListenPort, &v.Upstream, &v.Protocol, &enabled, &v.Version); err != nil {
			fail(w, 500, errors.New("无法读取端口转发规则"))
			return
		}
		v.Enabled = enabled == 1
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		fail(w, 500, errors.New("无法读取端口转发规则"))
		return
	}
	rows.Close()
	a.forwardRuntimeStates(r.Context(), out)
	writeJSON(w, 200, out)
}

func validateLayer4Rule(v *Layer4Rule) error {
	v.Name = strings.TrimSpace(v.Name)
	if v.Name == "" {
		v.Name = fmt.Sprintf("转发 %d", v.ListenPort)
	}
	if len(v.Name) > 100 {
		return errors.New("规则名称过长")
	}
	v.ListenAddress = strings.TrimSpace(v.ListenAddress)
	if v.ListenAddress == "" {
		v.ListenAddress = "0.0.0.0"
	}
	ip := net.ParseIP(v.ListenAddress)
	if ip == nil {
		return errors.New("监听地址必须是 IPv4 或 IPv6 地址")
	}
	v.ListenAddress = ip.String()
	if v.ListenPort < 1 || v.ListenPort > 65535 {
		return errors.New("监听端口需在 1-65535 之间")
	}
	switch v.Protocol {
	case "tcp", "udp", "both":
	default:
		return errors.New("协议只能是 TCP、UDP 或两者")
	}
	v.Upstream = strings.TrimSpace(v.Upstream)
	host, port, err := net.SplitHostPort(v.Upstream)
	if err != nil || host == "" {
		return errors.New("上游必须是 host:port（IPv6 需加方括号）")
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return errors.New("上游端口需在 1-65535 之间")
	}
	if strings.ContainsAny(v.Upstream, "\r\n\x00") {
		return errors.New("上游地址无效")
	}
	return nil
}

func (a *App) saveLayer4Rule(w http.ResponseWriter, r *http.Request, id int64, create bool) {
	a.listenerMu.Lock()
	defer a.listenerMu.Unlock()
	var v Layer4Rule
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&v); err != nil {
		fail(w, 400, errors.New("请求内容不是有效的 JSON"))
		return
	}
	if err := validateLayer4Rule(&v); err != nil {
		fail(w, 400, err)
		return
	}
	if v.Enabled {
		if err := a.checkForwardSocket(r.Context(), id, v); err != nil {
			fail(w, 409, err)
			return
		}
		if err := a.checkLayer4Conflict(r, id, v); err != nil {
			fail(w, 409, err)
			return
		}
	}
	var result int64
	var err error
	if create {
		res, e := a.db.ExecContext(r.Context(), `INSERT INTO layer4_rules(name,listen_address,listen_port,upstream,protocol,enabled) VALUES(?,?,?,?,?,?)`, v.Name, v.ListenAddress, v.ListenPort, v.Upstream, v.Protocol, boolInt(v.Enabled))
		err = e
		if err == nil {
			result, _ = res.LastInsertId()
		}
	} else {
		res, e := a.db.ExecContext(r.Context(), `UPDATE layer4_rules SET name=?,listen_address=?,listen_port=?,upstream=?,protocol=?,enabled=?,version=version+1,updated_at=CURRENT_TIMESTAMP WHERE id=? AND version=?`, v.Name, v.ListenAddress, v.ListenPort, v.Upstream, v.Protocol, boolInt(v.Enabled), id, v.Version)
		err = e
		if err == nil {
			n, _ := res.RowsAffected()
			if n == 0 {
				var exists int
				if queryErr := a.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM layer4_rules WHERE id=?`, id).Scan(&exists); queryErr != nil {
					fail(w, 500, errors.New("无法保存端口转发规则"))
					return
				}
				if exists == 0 {
					fail(w, 404, errors.New("端口转发规则不存在"))
					return
				}
				fail(w, 409, errors.New("该转发已被其他会话修改，请刷新后重新编辑"))
				return
			}
			result = id
		}
	}
	if err != nil {
		fail(w, 500, errors.New("无法保存端口转发规则"))
		return
	}
	if create {
		id = result
	}
	if err := a.apply(r.Context()); err != nil {
		writeJSON(w, 502, map[string]any{"id": id, "error": "配置已保存，但尚未生效。请重试应用或在配置历史中恢复。", "saved": true, "applied": false, "code": "configuration_pending"})
		return
	}
	v.ID = id
	if create {
		v.Version = 1
	} else {
		v.Version++
	}
	writeJSON(w, map[bool]int{true: 201, false: 200}[create], v)
}

func (a *App) checkLayer4Conflict(r *http.Request, id int64, v Layer4Rule) error {
	listeners, err := a.webListeners(r.Context())
	if err != nil {
		return err
	}
	for _, listener := range listeners {
		if listener.Port == v.ListenPort && (v.Protocol != "udp" || listener.Protocol == "https") {
			return fmt.Errorf("端口 %d 已被 Web 监听占用", v.ListenPort)
		}
	}
	apiListen := env("VIYA_LISTEN", "127.0.0.1:8181")
	for _, reserved := range []struct{ name, address string }{{"Caddy Admin API", a.adminListen}, {"Caddy HTTP", a.httpListen}, {"Viya management API", apiListen}} {
		if v.Protocol == "udp" {
			continue
		} // These management listeners are TCP, not UDP.
		rh, rp, e1 := net.SplitHostPort(reserved.address)
		if e1 != nil {
			continue
		}
		port, _ := strconv.Atoi(rp)
		if port != v.ListenPort {
			continue
		}
		rip := net.ParseIP(rh)
		lip := net.ParseIP(v.ListenAddress)
		if rh == "" || rip != nil && (rip.IsUnspecified() || rip.Equal(lip) || lip.IsUnspecified()) {
			return fmt.Errorf("listener conflicts with reserved %s port %d", reserved.name, v.ListenPort)
		}
	}
	rows, err := a.db.QueryContext(r.Context(), `SELECT id,listen_address,listen_port,protocol FROM layer4_rules WHERE enabled=1 AND id<>?`, id)
	if err != nil {
		return errors.New("无法检查监听冲突")
	}
	defer rows.Close()
	protocols := func(p string) map[string]bool {
		m := map[string]bool{}
		if p == "both" {
			m["tcp"] = true
			m["udp"] = true
		} else {
			m[p] = true
		}
		return m
	}
	for rows.Next() {
		var other int64
		var addr string
		var port int
		var proto string
		if rows.Scan(&other, &addr, &port, &proto) != nil {
			return errors.New("无法检查监听冲突")
		}
		if port != v.ListenPort {
			continue
		}
		ip1, ip2 := net.ParseIP(addr), net.ParseIP(v.ListenAddress)
		overlaps := ip1.Equal(ip2) || ip1.IsUnspecified() || ip2.IsUnspecified()
		if !overlaps {
			continue
		}
		for p := range protocols(v.Protocol) {
			if protocols(proto)[p] {
				return fmt.Errorf("listener conflicts with existing %s rule on %s:%d", p, addr, port)
			}
		}
	}
	return rows.Err()
}
func (a *App) createLayer4Rule(w http.ResponseWriter, r *http.Request) {
	a.saveLayer4Rule(w, r, 0, true)
}
func (a *App) updateLayer4Rule(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		fail(w, 400, errors.New("端口转发规则 ID 无效"))
		return
	}
	a.saveLayer4Rule(w, r, id, false)
}
func (a *App) deleteLayer4Rule(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		fail(w, 400, errors.New("端口转发规则 ID 无效"))
		return
	}
	res, err := a.db.ExecContext(r.Context(), `DELETE FROM layer4_rules WHERE id=?`, id)
	if err != nil {
		fail(w, 500, errors.New("无法删除规则"))
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		fail(w, 404, errors.New("端口转发规则不存在"))
		return
	}
	if err := a.apply(r.Context()); err != nil {
		failSavedApplication(w)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func l4Protocols(p string) []string {
	if p == "both" {
		return []string{"tcp", "udp"}
	}
	return []string{p}
}
func l4Dial(protocol, upstream string) string { return protocol + "/" + upstream }

func (a *App) toggleLayer4Enabled(w http.ResponseWriter, r *http.Request) {
	id, e := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var input struct {
		Enabled *bool `json:"enabled"`
		Version int64 `json:"version"`
	}
	if e != nil || id < 1 || json.NewDecoder(http.MaxBytesReader(w, r.Body, 2048)).Decode(&input) != nil || input.Enabled == nil {
		fail(w, 400, errors.New("转发状态无效"))
		return
	}
	a.listenerMu.Lock()
	defer a.listenerMu.Unlock()
	a.mu.Lock()
	defer a.mu.Unlock()
	var v Layer4Rule
	var enabled int
	e = a.db.QueryRowContext(r.Context(), `SELECT listen_address,listen_port,upstream,protocol,enabled,version FROM layer4_rules WHERE id=?`, id).Scan(&v.ListenAddress, &v.ListenPort, &v.Upstream, &v.Protocol, &enabled, &v.Version)
	if e != nil {
		fail(w, 404, errors.New("转发规则不存在"))
		return
	}
	if v.Version != input.Version {
		fail(w, 409, errors.New("转发规则已被修改，请刷新后重试"))
		return
	}
	if *input.Enabled {
		if e = a.checkLayer4Conflict(r, id, v); e != nil {
			fail(w, 409, e)
			return
		}
	}
	res, e := a.db.ExecContext(r.Context(), `UPDATE layer4_rules SET enabled=?,version=version+1,updated_at=CURRENT_TIMESTAMP WHERE id=? AND version=?`, boolInt(*input.Enabled), id, v.Version)
	if e != nil {
		fail(w, 500, e)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		fail(w, 409, errors.New("转发规则已被修改，请刷新后重试"))
		return
	}
	if e = a.applyLocked(r.Context()); e != nil {
		rollbackCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		_, rollbackErr := a.db.ExecContext(rollbackCtx, `UPDATE layer4_rules SET enabled=?,version=version+1 WHERE id=?`, enabled, id)
		if rollbackErr != nil {
			slog.Error("rollback layer4 enabled state", "id", id, "error", rollbackErr)
			cancel()
			fail(w, 500, errors.New("转发应用失败，且未能恢复原状态"))
			return
		}
		if rbErr := a.applyLocked(rollbackCtx); rbErr != nil {
			slog.Error("reapply after layer4 rollback", "error", rbErr)
		}
		cancel()
		fail(w, 502, errors.New("转发未能应用，已恢复原状态"))
		return
	}
	writeJSON(w, 200, map[string]any{"enabled": *input.Enabled, "version": v.Version + 1})
}
