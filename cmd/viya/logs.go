package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type IPBan struct {
	ConfirmRisk bool   `json:"confirm_risk,omitempty"`
	Source      string `json:"source"`
	ID          int64  `json:"id"`
	Domain      string `json:"domain"`
	IP          string `json:"ip"`
	Reason      string `json:"reason"`
	ExpiresAt   int64  `json:"expires_at"`
	CreatedAt   string `json:"created_at"`
}

func (a *App) recentLogs(w http.ResponseWriter, r *http.Request) {
	query, err := parseLogQuery(r)
	if err != nil {
		fail(w, 400, err)
		return
	}
	events, err := readRecentLogFileWhere(filepathJoin(a.dataDir, "access.log"), query.Limit, func(event map[string]any) bool { return caddyLogKind(event) == "access" && query.matches(event) })
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		fail(w, 500, errors.New("无法读取访问日志"))
		return
	}
	if events == nil {
		events = []map[string]any{}
	}
	writeJSON(w, 200, events)
}

// layer4RuleLogs returns the engine entries Caddy emitted for one forwarding
// rule. Layer4 has no per-request access record, so these are the connection
// errors, reload notices and (once logging is enabled) per-connection stats.
func (a *App) layer4RuleLogs(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		fail(w, 400, errors.New("无效转发编号"))
		return
	}
	var port int
	if err = a.db.QueryRowContext(r.Context(), `SELECT listen_port FROM layer4_rules WHERE id=?`, id).Scan(&port); err != nil {
		fail(w, 404, errors.New("转发规则不存在"))
		return
	}
	events, err := readRecentLogFileWhere(filepathJoin(a.dataDir, "l4-access.log"), 600, func(event map[string]any) bool {
		return caddyLogKind(event) == "layer4" && logEventPort(event["local"]) == port
	})
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		fail(w, 500, errors.New("无法读取转发日志"))
		return
	}
	out := []map[string]any{}
	for i := len(events) - 1; i >= 0 && len(out) < 50; i-- {
		out = append(out, events[i])
	}
	writeJSON(w, 200, out)
}

// logEventPort reads the port out of Caddy's "host:port" / "[v6]:port" values.
func logEventPort(value any) int {
	text, _ := value.(string)
	if text == "" {
		return 0
	}
	_, portText, err := net.SplitHostPort(text)
	if err != nil {
		return 0
	}
	port, _ := strconv.Atoi(portText)
	return port
}

func (a *App) streamLogs(w http.ResponseWriter, r *http.Request) {
	path := filepathJoin(a.dataDir, "access.log")
	file, err := os.Open(path)
	if err != nil {
		http.Error(w, "access log is not ready", http.StatusServiceUnavailable)
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		http.Error(w, "access log is not ready", 500)
		return
	}
	offset := info.Size()
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming is unavailable", 500)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(200)
	_, _ = io.WriteString(w, "retry: 2000\n\n")
	flusher.Flush()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	deadline := time.NewTimer(24 * time.Second)
	defer deadline.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-deadline.C:
			return
		case <-ticker.C:
			lines, newOffset, e := readLogLines(path, offset)
			if e != nil {
				continue
			}
			offset = newOffset
			for _, line := range lines {
				var event map[string]any
				if json.Unmarshal(line, &event) != nil {
					continue
				}
				sanitizeAccessEvent(event)
				b, _ := json.Marshal(event)
				if _, e = fmt.Fprintf(w, "data: %s\n\n", b); e != nil {
					return
				}
				flusher.Flush()
			}
		}
	}
}

// caddyLogKind separates HTTP access records from the engine and layer4
// entries Caddy writes into the same file. Rendering a layer4 error with the
// HTTP columns produced empty rows, so every reader filters by kind first.
func caddyLogKind(event map[string]any) string {
	if logger, ok := event["logger"].(string); ok {
		switch {
		case strings.HasPrefix(logger, "http.log.access"):
			return "access"
		case logger == "layer4":
			return "layer4"
		}
	}
	if _, ok := event["request"].(map[string]any); ok {
		return "access"
	}
	return "other"
}

func readRecentLogFileWhere(path string, limit int, keep func(map[string]any) bool) ([]map[string]any, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	start := int64(0)
	if info.Size() > 2<<20 {
		start = info.Size() - (2 << 20)
		_, _ = f.Seek(start, io.SeekStart)
	}
	reader := bufio.NewReader(f)
	if start > 0 {
		_, _ = reader.ReadString('\n')
	}
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), 1<<20)
	ring := make([]map[string]any, 0, limit)
	for scanner.Scan() {
		var event map[string]any
		if json.Unmarshal(scanner.Bytes(), &event) != nil {
			continue
		}
		sanitizeAccessEvent(event)
		if keep != nil && !keep(event) {
			continue
		}
		if len(ring) == limit {
			copy(ring, ring[1:])
			ring = ring[:limit-1]
		}
		ring = append(ring, event)
	}
	return ring, scanner.Err()
}
func readLogLines(path string, offset int64) ([][]byte, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, offset, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, offset, err
	}
	if info.Size() < offset {
		offset = 0
	}
	if _, err = f.Seek(offset, io.SeekStart); err != nil {
		return nil, offset, err
	}
	reader := bufio.NewReader(f)
	lines := [][]byte{}
	consumed := int64(0)
	for {
		line, e := reader.ReadBytes('\n')
		if e == nil {
			lines = append(lines, line)
			consumed += int64(len(line))
			continue
		}
		if e == io.EOF {
			// A trailing fragment without a newline means Caddy is still
			// writing it; leave the offset where it is so the next read sees
			// the complete line instead of losing it.
			break
		}
		return lines, offset + consumed, e
	}
	return lines, offset + consumed, nil
}
func sanitizeAccessEvent(event map[string]any) {
	if headers, ok := event["resp_headers"].(map[string]any); ok {
		for key, value := range headers {
			if strings.EqualFold(key, "X-Viya-Access-Rule") {
				var raw string
				switch v := value.(type) {
				case []any:
					if len(v) > 0 {
						raw, _ = v[0].(string)
					}
				case []string:
					if len(v) > 0 {
						raw = v[0]
					}
				case string:
					raw = v
				}
				if id, e := strconv.ParseInt(raw, 10, 64); e == nil && id > 0 {
					event["access_rule_id"] = id
				}
			}
		}
	}
	delete(event, "resp_headers")
	if request, ok := event["request"].(map[string]any); ok {
		if uri, ok := request["uri"].(string); ok {
			if parsed, e := url.Parse(uri); e == nil {
				parsed.RawQuery = ""
				parsed.ForceQuery = false
				request["uri"] = parsed.String()
			}
		}
		delete(request, "headers")
		delete(request, "headers.Authorization")
		delete(request, "headers.Cookie")
	}
	delete(event, "headers")
}
func filepathJoin(base, name string) string {
	return strings.TrimRight(base, "/\\") + string(os.PathSeparator) + name
}

func (a *App) watchAccessLogs() {
	path := filepathJoin(a.dataDir, "access.log")
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	// Start at the current end when logs already exist; if Caddy creates the
	// file just after startup, begin at byte zero so early requests are not lost.
	offset := int64(0)
	if info, err := os.Stat(path); err == nil {
		offset = info.Size()
	}
	for {
		select {
		case <-a.ctx.Done():
			return
		case <-ticker.C:

			_, err := os.Stat(path)
			if err != nil {
				continue
			}
			lines, next, err := readLogLines(path, offset)
			if err != nil {
				continue
			}
			offset = next
			changed := false
			for _, line := range lines {
				if a.observeAttack(line) {
					changed = true
				}
			}
			if changed {
				// Coalesced + throttled: a burst of denied requests triggers one
				// reload, and automatic bans can't fork/reload more often than
				// once per 15s (see scheduleAutoBanApply).
				a.scheduleAutoBanApply()
			}
		}
	}
}
func (a *App) observeAttack(line []byte) bool {
	policy := a.loadBanPolicy()
	if !policy.Enabled {
		return false
	}
	var event map[string]any
	if json.Unmarshal(line, &event) != nil {
		return false
	}
	status := intValue(event["status"])
	if headers, ok := event["resp_headers"].(map[string]any); ok {
		for key := range headers {
			if strings.EqualFold(key, "X-Viya-Access-Rule") {
				return false
			}
		}
	}
	if status != 400 && status != 401 && status != 403 {
		return false
	}
	request, _ := event["request"].(map[string]any)
	if request == nil {
		return false
	}
	host, _ := request["host"].(string)
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
	remote, _ := request["remote_ip"].(string)
	if remote == "" {
		remote, _ = request["client_ip"].(string)
	}
	if h, _, err := net.SplitHostPort(remote); err == nil {
		remote = h
	}
	ip := net.ParseIP(remote)
	if ip == nil || ip.IsLoopback() || host == "" {
		return false
	}
	if policy.allows(ip) {
		return false
	}
	remote = ip.String()
	window := time.Duration(policy.WindowMin) * time.Minute
	now := time.Now()
	key := host + "\x00" + remote
	recent := a.failCounts[key][:0]
	for _, at := range a.failCounts[key] {
		if now.Sub(at) < window {
			recent = append(recent, at)
		}
	}
	recent = append(recent, now)
	a.failCounts[key] = recent
	if len(a.failCounts) > 10000 {
		for k, times := range a.failCounts {
			if len(times) == 0 || now.Sub(times[len(times)-1]) > 10*time.Minute {
				delete(a.failCounts, k)
			}
		}
	}
	if len(recent) < policy.Threshold {
		return false
	}
	a.failCounts[key] = nil
	// A manual IP/range rule must retain the administrator's chosen expiry.
	manualRows, e := a.db.QueryContext(a.ctx, `SELECT ip FROM ip_bans WHERE domain=? AND source='manual' AND (expires_at=0 OR expires_at>?)`, host, now.Unix())
	if e != nil {
		return false
	}
	covered := false
	for manualRows.Next() {
		var value string
		if manualRows.Scan(&value) != nil {
			continue
		}
		if stored := net.ParseIP(value); stored != nil && stored.Equal(ip) {
			covered = true
		}
		if _, network, e := net.ParseCIDR(value); e == nil && network.Contains(ip) {
			covered = true
		}
	}
	manualRows.Close()
	if covered {
		return false
	}
	ranges, _ := normalizeCIDRs([]string{remote})
	if len(ranges) != 1 {
		return false
	}
	banFor := time.Duration(policy.BanMin) * time.Minute
	_, err := a.db.ExecContext(a.ctx, `INSERT INTO ip_bans(domain,ip,reason,expires_at) VALUES(?,?,?,?) ON CONFLICT(domain,ip) DO UPDATE SET reason=excluded.reason,expires_at=excluded.expires_at WHERE ip_bans.source='auto'`, host, ranges[0], fmt.Sprintf("%d denied client requests within %d minutes", policy.Threshold, policy.WindowMin), now.Add(banFor).Unix())
	return err == nil
}
func intValue(value any) int {
	switch v := value.(type) {
	case float64:
		return int(v)
	case json.Number:
		n, err := strconv.Atoi(string(v))
		if err == nil {
			return n
		}
	case int:
		return v
	}
	return 0
}
func (a *App) listIPBans(w http.ResponseWriter, r *http.Request) {
	rows, e := a.db.QueryContext(r.Context(), `SELECT id,domain,ip,reason,expires_at,created_at,source FROM ip_bans WHERE expires_at=0 OR expires_at>? ORDER BY expires_at`, time.Now().Unix())
	if e != nil {
		fail(w, 500, errors.New("无法读取 IP 封禁"))
		return
	}
	defer rows.Close()
	out := []IPBan{}
	for rows.Next() {
		var v IPBan
		if rows.Scan(&v.ID, &v.Domain, &v.IP, &v.Reason, &v.ExpiresAt, &v.CreatedAt, &v.Source) != nil {
			fail(w, 500, errors.New("无法读取 IP 封禁"))
			return
		}
		out = append(out, v)
	}
	writeJSON(w, 200, out)
}
func (a *App) removeIPBan(w http.ResponseWriter, r *http.Request) {
	id, e := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if e != nil || id < 1 {
		fail(w, 400, errors.New("封禁 ID 无效"))
		return
	}
	if pending := a.pendingAccessConfirmation(r.Context()); pending != nil {
		var guarded int64
		_ = a.db.QueryRowContext(r.Context(), `SELECT ban_id FROM access_rule_guards WHERE id=1`).Scan(&guarded)
		if guarded == id {
			fail(w, 409, errors.New("该规则正在限时确认，请使用取消修改恢复原规则"))
			return
		}
	}
	res, e := a.db.ExecContext(r.Context(), `DELETE FROM ip_bans WHERE id=?`, id)
	if e != nil {
		fail(w, 500, errors.New("无法移除 IP 封禁"))
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		fail(w, 404, errors.New("IP 封禁不存在"))
		return
	}
	if e = a.apply(r.Context()); e != nil {
		failSavedApplication(w)
		return
	}
	w.WriteHeader(204)
}
