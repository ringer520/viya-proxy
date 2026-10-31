package main

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Monitor struct {
	ID        int64           `json:"id"`
	Name      string          `json:"name"`
	Kind      string          `json:"kind"`
	Target    string          `json:"target"`
	Interval  int             `json:"interval"`
	Timeout   int             `json:"timeout"`
	Enabled   bool            `json:"enabled"`
	Status    string          `json:"status"`
	Latency   float64         `json:"latency"`
	LastCheck int64           `json:"last_check"`
	Error     string          `json:"error"`
	History   []MonitorSample `json:"history"`
	Uptime    float64         `json:"uptime"`
}
type MonitorSample struct {
	OK      bool    `json:"ok"`
	Latency float64 `json:"latency"`
	At      int64   `json:"at"`
}

func (a *App) initMonitors() error {
	_, err := a.db.Exec(`CREATE TABLE IF NOT EXISTS monitors(id INTEGER PRIMARY KEY,name TEXT NOT NULL,kind TEXT NOT NULL,target TEXT NOT NULL,interval INTEGER NOT NULL DEFAULT 60,timeout INTEGER NOT NULL DEFAULT 5,enabled INTEGER NOT NULL DEFAULT 1,next_check INTEGER NOT NULL DEFAULT 0,last_check INTEGER NOT NULL DEFAULT 0,status TEXT NOT NULL DEFAULT 'pending',latency REAL NOT NULL DEFAULT 0,error TEXT NOT NULL DEFAULT '');CREATE TABLE IF NOT EXISTS monitor_samples(id INTEGER PRIMARY KEY,monitor_id INTEGER NOT NULL REFERENCES monitors(id) ON DELETE CASCADE,ok INTEGER NOT NULL,latency REAL NOT NULL,at INTEGER NOT NULL);CREATE INDEX IF NOT EXISTS idx_monitor_samples ON monitor_samples(monitor_id,id DESC)`)
	return err
}
func validateMonitor(m *Monitor) error {
	m.Name = strings.TrimSpace(m.Name)
	m.Target = strings.TrimSpace(m.Target)
	if m.Name == "" || len(m.Name) > 100 {
		return errors.New("监控名称须为 1–100 字节")
	}
	if m.Interval == 0 {
		m.Interval = 60
	}
	if m.Timeout == 0 {
		m.Timeout = 5
	}
	if m.Interval < 15 || m.Interval > 3600 || m.Timeout < 1 || m.Timeout > 10 {
		return errors.New("探测间隔 15–3600 秒，超时 1–10 秒")
	}
	switch m.Kind {
	case "http":
		u, e := url.Parse(m.Target)
		if e != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.Fragment != "" {
			return errors.New("填写无凭据的 HTTP(S) 地址")
		}
	case "tcp":
		host, port, e := net.SplitHostPort(m.Target)
		p, pe := strconv.Atoi(port)
		if e != nil || host == "" || pe != nil || p < 1 || p > 65535 {
			return errors.New("TCP 目标须为 host:port，IPv6 需方括号")
		}
	default:
		return errors.New("监控类型须为 HTTP 或 TCP")
	}
	return nil
}
func (a *App) listMonitors(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.QueryContext(r.Context(), `SELECT id,name,kind,target,interval,timeout,enabled,status,latency,last_check,error FROM monitors ORDER BY id`)
	if err != nil {
		fail(w, 500, err)
		return
	}
	out := []Monitor{}
	for rows.Next() {
		var m Monitor
		var enabled int
		if err = rows.Scan(&m.ID, &m.Name, &m.Kind, &m.Target, &m.Interval, &m.Timeout, &enabled, &m.Status, &m.Latency, &m.LastCheck, &m.Error); err != nil {
			rows.Close()
			fail(w, 500, err)
			return
		}
		m.Enabled = enabled == 1
		m.History = []MonitorSample{}
		out = append(out, m)
	}
	rows.Close()
	for i := range out {
		samples, e := a.db.QueryContext(r.Context(), `SELECT ok,latency,at FROM monitor_samples WHERE monitor_id=? ORDER BY id DESC LIMIT 40`, out[i].ID)
		if e != nil {
			fail(w, 500, e)
			return
		}
		up := 0
		for samples.Next() {
			var sample MonitorSample
			var ok int
			if e = samples.Scan(&ok, &sample.Latency, &sample.At); e != nil {
				samples.Close()
				fail(w, 500, e)
				return
			}
			sample.OK = ok == 1
			if sample.OK {
				up++
			}
			out[i].History = append(out[i].History, sample)
		}
		samples.Close()
		if len(out[i].History) > 0 {
			out[i].Uptime = 100 * float64(up) / float64(len(out[i].History))
		}
	}
	writeJSON(w, 200, out)
}
func (a *App) saveMonitor(w http.ResponseWriter, r *http.Request) {
	a.monitorMu.Lock()
	defer a.monitorMu.Unlock()
	a.monitorGeneration.Add(1)
	var m Monitor
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&m) != nil {
		fail(w, 400, errors.New("监控格式无效"))
		return
	}
	if e := validateMonitor(&m); e != nil {
		fail(w, 400, e)
		return
	}
	id := r.PathValue("id")
	if id == "" {
		res, e := a.db.ExecContext(r.Context(), `INSERT INTO monitors(name,kind,target,interval,timeout,enabled) VALUES(?,?,?,?,?,?)`, m.Name, m.Kind, m.Target, m.Interval, m.Timeout, boolInt(m.Enabled))
		if e != nil {
			fail(w, 500, e)
			return
		}
		m.ID, _ = res.LastInsertId()
	} else {
		n, e := strconv.ParseInt(id, 10, 64)
		if e != nil || n < 1 {
			fail(w, 400, errors.New("invalid monitor id"))
			return
		}
		m.ID = n
		res, e := a.db.ExecContext(r.Context(), `UPDATE monitors SET name=?,kind=?,target=?,interval=?,timeout=?,enabled=?,next_check=0,status='pending',last_check=0,latency=0,error='' WHERE id=?`, m.Name, m.Kind, m.Target, m.Interval, m.Timeout, boolInt(m.Enabled), n)
		if e != nil {
			fail(w, 500, e)
			return
		}
		count, _ := res.RowsAffected()
		if count == 0 {
			fail(w, 404, errors.New("监控不存在"))
			return
		}
		_, _ = a.db.ExecContext(r.Context(), `DELETE FROM monitor_samples WHERE monitor_id=?`, n)
	}
	writeJSON(w, 200, m)
}
func (a *App) deleteMonitor(w http.ResponseWriter, r *http.Request) {
	a.monitorMu.Lock()
	defer a.monitorMu.Unlock()
	a.monitorGeneration.Add(1)
	id, e := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if e != nil || id < 1 {
		fail(w, 400, errors.New("invalid monitor id"))
		return
	}
	res, e := a.db.ExecContext(r.Context(), `DELETE FROM monitors WHERE id=?`, id)
	if e != nil {
		fail(w, 500, e)
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		fail(w, 404, errors.New("监控不存在"))
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}
func (a *App) probeMonitors(ctx context.Context, force bool) {
	generation := a.monitorGeneration.Load()
	query := `SELECT id,name,kind,target,interval,timeout FROM monitors WHERE enabled=1 AND next_check<=? ORDER BY id LIMIT 40`
	at := time.Now().Unix()
	if force {
		at = 1 << 62
	}
	rows, e := a.db.QueryContext(ctx, query, at)
	if e != nil {
		return
	}
	list := []Monitor{}
	for rows.Next() {
		var m Monitor
		if rows.Scan(&m.ID, &m.Name, &m.Kind, &m.Target, &m.Interval, &m.Timeout) == nil {
			list = append(list, m)
		}
	}
	rows.Close()
	var workers sync.WaitGroup
	slots := make(chan struct{}, 4)
	for _, item := range list {
		slots <- struct{}{}
		workers.Add(1)
		go func(m Monitor) {
			defer workers.Done()
			defer func() { <-slots }()
			if ctx.Err() != nil {
				return
			}
			probeCtx, cancel := context.WithTimeout(ctx, time.Duration(m.Timeout)*time.Second)
			started := time.Now()
			var err error
			if m.Kind == "tcp" {
				var conn net.Conn
				conn, err = (&net.Dialer{}).DialContext(probeCtx, "tcp", m.Target)
				if conn != nil {
					conn.Close()
				}
			} else {
				var req *http.Request
				req, err = http.NewRequestWithContext(probeCtx, "GET", m.Target, nil)
				if err == nil {
					client := http.Client{Timeout: time.Duration(m.Timeout) * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
						if len(via) >= 3 {
							return errors.New("跳转次数超过 3 次")
						}
						return nil
					}}
					var res *http.Response
					res, err = client.Do(req)
					if res != nil {
						res.Body.Close()
						if err == nil && (res.StatusCode < 200 || res.StatusCode >= 400) {
							err = errors.New("HTTP 状态 " + res.Status)
						}
					}
				}
			}
			cancel()
			ms := float64(time.Since(started).Microseconds()) / 1000
			now := time.Now().Unix()
			status, message := "up", ""
			if err != nil {
				status = "down"
				message = err.Error()
			}
			a.monitorMu.Lock()
			defer a.monitorMu.Unlock()
			if generation != a.monitorGeneration.Load() {
				return
			}
			tx, e := a.db.BeginTx(ctx, nil)
			if e != nil {
				return
			}
			_, e = tx.ExecContext(ctx, `UPDATE monitors SET status=?,latency=?,error=?,last_check=?,next_check=? WHERE id=?`, status, ms, message, now, now+int64(m.Interval), m.ID)
			if e == nil {
				_, e = tx.ExecContext(ctx, `INSERT INTO monitor_samples(monitor_id,ok,latency,at) VALUES(?,?,?,?)`, m.ID, boolInt(err == nil), ms, now)
			}
			if e == nil {
				_, e = tx.ExecContext(ctx, `DELETE FROM monitor_samples WHERE monitor_id=? AND id NOT IN(SELECT id FROM monitor_samples WHERE monitor_id=? ORDER BY id DESC LIMIT 40)`, m.ID, m.ID)
			}
			if e != nil {
				tx.Rollback()
			} else {
				tx.Commit()
			}
		}(item)
	}
	workers.Wait()
}
func (a *App) monitorWorker() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-a.ctx.Done():
			return
		case <-ticker.C:
			a.probeMonitors(a.ctx, false)
		}
	}
}
func (a *App) checkMonitors(w http.ResponseWriter, r *http.Request) { // Queue work; avoid holding an HTTP connection for a batch of slow targets.
	_, err := a.db.ExecContext(r.Context(), `UPDATE monitors SET next_check=0 WHERE enabled=1`)
	if err != nil {
		fail(w, 500, err)
		return
	}
	writeJSON(w, 202, map[string]string{"message": "已安排立即检测，后台将在 5 秒内开始"})
}
