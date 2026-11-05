package main

import (
	"bufio"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type accessEntry struct {
	Logger   string  `json:"logger"`
	Time     float64 `json:"ts"`
	Status   int     `json:"status"`
	Upload   int64   `json:"bytes_read"`
	Download int64   `json:"size"`
	Duration float64 `json:"duration"`
	Request  struct {
		Host     string `json:"host"`
		Method   string `json:"method"`
		URI      string `json:"uri"`
		RemoteIP string `json:"remote_ip"`
		ClientIP string `json:"client_ip"`
	} `json:"request"`
}

// layer4ConnStat is the per-connection accounting Caddy's layer4 app writes at
// debug level once a connection closes. read maps to client→upstream bytes and
// written to upstream→client bytes; local is "host:port" and network is the
// lower-case protocol name ("tcp"/"udp").
type layer4ConnStat struct {
	Msg     string `json:"msg"`
	Network string `json:"network"`
	Local   string `json:"local"`
	Read    uint64 `json:"read"`
	Written uint64 `json:"written"`
}
type trafficCounters struct {
	Requests int64 `json:"requests"`
	Errors   int64 `json:"errors"`
	Upload   int64 `json:"upload"`
	Download int64 `json:"download"`
}
type trafficPoint struct {
	Hour int64 `json:"hour"`
	trafficCounters
}
type trafficSummary struct {
	Total  trafficCounters `json:"total"`
	Recent trafficCounters `json:"recent"`
	Hourly []trafficPoint  `json:"hourly"`
	// Daily is populated for the 7d and 30d ranges, where 720 hourly points
	// would be more resolution than the panel can draw.
	Daily []trafficDay `json:"daily,omitempty"`
}
type trafficDay struct {
	Day int64 `json:"day"`
	trafficCounters
}

func (a *App) initTraffic() error {
	_, err := a.db.Exec(`CREATE TABLE IF NOT EXISTS traffic_hours(site_id INTEGER NOT NULL REFERENCES proxy_hosts(id) ON DELETE CASCADE,hour INTEGER NOT NULL,requests INTEGER NOT NULL,errors INTEGER NOT NULL,upload INTEGER NOT NULL,download INTEGER NOT NULL,PRIMARY KEY(site_id,hour));
	CREATE INDEX IF NOT EXISTS idx_traffic_hours_hour ON traffic_hours(hour);
	CREATE TABLE IF NOT EXISTS traffic_cursors(generation TEXT PRIMARY KEY,offset INTEGER NOT NULL,complete INTEGER NOT NULL DEFAULT 0);
	CREATE TABLE IF NOT EXISTS traffic_files(path TEXT PRIMARY KEY,size INTEGER NOT NULL,mtime INTEGER NOT NULL,offset INTEGER NOT NULL DEFAULT 0,complete INTEGER NOT NULL DEFAULT 0);
	CREATE TABLE IF NOT EXISTS site_access_events(id INTEGER PRIMARY KEY,site_id INTEGER NOT NULL REFERENCES proxy_hosts(id) ON DELETE CASCADE,event TEXT NOT NULL);
	CREATE INDEX IF NOT EXISTS idx_site_access_events ON site_access_events(site_id,id DESC);
	CREATE TABLE IF NOT EXISTS layer4_traffic_hours(rule_id INTEGER NOT NULL REFERENCES layer4_rules(id) ON DELETE CASCADE,hour INTEGER NOT NULL,connections INTEGER NOT NULL,upload INTEGER NOT NULL,download INTEGER NOT NULL,PRIMARY KEY(rule_id,hour));
	CREATE INDEX IF NOT EXISTS idx_layer4_traffic_hours_hour ON layer4_traffic_hours(hour);`)
	return err
}
func normalizedLogHost(host string) string {
	if name, _, err := net.SplitHostPort(host); err == nil {
		host = name
	}
	return strings.ToLower(strings.TrimSuffix(host, "."))
}
func matchLogSite(host string, sites map[string]int64) int64 {
	host = normalizedLogHost(host)
	if id := sites[host]; id != 0 {
		return id
	}
	// Caddy wildcard host matchers cover exactly one DNS label.
	if dot := strings.IndexByte(host, '.'); dot > 0 {
		return sites["*"+host[dot:]]
	}
	return 0
}
func (a *App) watchTraffic() {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	lastPrune := time.Now()
	for {
		if err := a.ingestTraffic(); err != nil && a.ctx.Err() == nil {
			slog.Warn("traffic log ingestion", "error", err)
		}
		if time.Since(lastPrune) > time.Hour {
			ctx, cancel := context.WithTimeout(a.ctx, 30*time.Second)
			if err := a.pruneTraffic(ctx); err != nil && a.ctx.Err() == nil {
				slog.Warn("traffic history cleanup", "error", err)
			}
			cancel()
			lastPrune = time.Now()
		}
		select {
		case <-a.ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Hourly statistics use a configurable bounded retention window.

func (a *App) pruneTraffic(ctx context.Context) error {
	policy := a.retention(ctx)
	cutoff := time.Now().Add(-time.Duration(policy.TrafficDays) * 24 * time.Hour).Unix()
	if _, err := a.db.ExecContext(ctx, `DELETE FROM traffic_hours WHERE hour<?`, cutoff); err != nil {
		return err
	}
	if _, err := a.db.ExecContext(ctx, `DELETE FROM layer4_traffic_hours WHERE hour<?`, cutoff); err != nil {
		return err
	}
	return pruneRecentAccess(ctx, a.db, policy.AccessEvents)
}
func (a *App) ingestTraffic() error {
	a.trafficMu.Lock()
	defer a.trafficMu.Unlock()
	var paths []string
	for _, pattern := range []string{"access*.log*", "l4*.log*"} {
		found, err := filepath.Glob(filepath.Join(a.dataDir, pattern))
		if err != nil {
			return err
		}
		paths = append(paths, found...)
	}
	sort.Strings(paths)
	for _, path := range paths {
		if !strings.HasSuffix(path, ".log") && !strings.HasSuffix(path, ".log.gz") {
			continue
		}
		if err := a.ingestTrafficFile(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}
func (a *App) ingestTrafficFile(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	// Rotated logs never change once complete; skip them without paying for a
	// gzip header decompression every two seconds.
	var indexedSize, indexedMtime int64
	var indexedComplete int
	if err := a.db.QueryRowContext(a.ctx, `SELECT size,mtime,complete FROM traffic_files WHERE path=?`, path).Scan(&indexedSize, &indexedMtime, &indexedComplete); err == nil {
		if indexedComplete != 0 && indexedSize == info.Size() && indexedMtime == info.ModTime().UnixNano() {
			return nil
		}
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	var source io.Reader = f
	if strings.HasSuffix(path, ".gz") {
		z, err := gzip.NewReader(f)
		if err != nil {
			return err
		}
		defer z.Close()
		source = z
	}
	reader := bufio.NewReaderSize(source, 64*1024)
	first, err := reader.ReadBytes('\n')
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err != nil {
		return err
	}
	// Content identity follows the same log through rename and gzip rotation.
	generation := fmt.Sprintf("%x", sha256.Sum256(first))
	var offset int64
	var complete int
	err = a.db.QueryRowContext(a.ctx, `SELECT offset,complete FROM traffic_cursors WHERE generation=?`, generation).Scan(&offset, &complete)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if complete != 0 {
		return nil
	}
	if offset > 0 {
		if offset < int64(len(first)) {
			return errors.New("invalid traffic cursor")
		}
		if strings.HasSuffix(path, ".gz") {
			if _, err = io.CopyN(io.Discard, reader, offset-int64(len(first))); err != nil {
				return err
			}
		} else {
			if _, err = f.Seek(offset, io.SeekStart); err != nil {
				return err
			}
			reader = bufio.NewReader(f)
		}
	} else {
		reader = bufio.NewReader(io.MultiReader(strings.NewReader(string(first)), reader))
	}
	rows, err := a.db.QueryContext(a.ctx, `SELECT id,domain,listener_id FROM proxy_hosts`)
	if err != nil {
		return err
	}
	sites := map[string]int64{}
	sitesByListener := map[string]map[string]int64{}
	ambiguous := map[string]bool{}
	allIDs := []int64{}
	for rows.Next() {
		var id int64
		var domain, listenerID string
		if err = rows.Scan(&id, &domain, &listenerID); err != nil {
			rows.Close()
			return err
		}
		if sitesByListener[listenerID] == nil {
			sitesByListener[listenerID] = map[string]int64{}
		}
		sitesByListener[listenerID][domain] = id
		allIDs = append(allIDs, id)
		if sites[domain] != 0 {
			delete(sites, domain)
			ambiguous[domain] = true
		}
		if !ambiguous[domain] {
			sites[domain] = id
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	// Map Caddy's layer4 connection lines back to rules. The log carries the
	// protocol as "network" and the listen address as "local", which together
	// disambiguate rules that share a port across tcp and udp.
	l4PortToRule := map[string]int64{}
	l4Rows, err := a.db.QueryContext(a.ctx, `SELECT id,listen_port,protocol FROM layer4_rules`)
	if err != nil {
		return err
	}
	for l4Rows.Next() {
		var id int64
		var port int
		var protocol string
		if err = l4Rows.Scan(&id, &port, &protocol); err != nil {
			l4Rows.Close()
			return err
		}
		for _, p := range l4Protocols(protocol) {
			l4PortToRule[p+":"+strconv.Itoa(port)] = id
		}
	}
	err = l4Rows.Err()
	l4Rows.Close()
	if err != nil {
		return err
	}
	eventLimit := a.retention(a.ctx).AccessEvents
	var resetAt string
	_ = a.db.QueryRowContext(a.ctx, `SELECT value FROM panel_settings WHERE key='traffic_reset_at'`).Scan(&resetAt)
	resetTime, _ := strconv.ParseFloat(resetAt, 64)
	tx, err := a.db.BeginTx(a.ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	start := offset
	for offset-start < 4<<20 {
		line, e := reader.ReadBytes('\n')
		if e == io.EOF {
			if filepath.Base(path) != "access.log" && filepath.Base(path) != "l4-access.log" {
				complete = 1
			}
			break
		}
		if e != nil {
			return e
		}
		offset += int64(len(line))
		var event accessEntry
		if json.Unmarshal(line, &event) != nil || event.Time <= 0 || event.Time < resetTime {
			continue
		}
		// Layer4 connection accounting is emitted under the "layer4" logger and
		// has no HTTP status; route it to the per-rule counters instead. Only the
		// "stopped" line carries byte counts — the "started" line is skipped.
		if event.Logger == "layer4" {
			var l4 layer4ConnStat
			if json.Unmarshal(line, &l4) != nil || l4.Local == "" || l4.Msg != "stopped handling connection; connection stats" {
				continue
			}
			ruleID := l4PortToRule[l4.Network+":"+strconv.Itoa(logEventPort(l4.Local))]
			if ruleID == 0 {
				continue
			}
			hour := int64(event.Time) / 3600 * 3600
			if _, err = tx.ExecContext(a.ctx, `INSERT INTO layer4_traffic_hours(rule_id,hour,connections,upload,download) VALUES(?,?,1,?,?) ON CONFLICT(rule_id,hour) DO UPDATE SET connections=connections+1,upload=upload+excluded.upload,download=download+excluded.download`, ruleID, hour, int64(l4.Read), int64(l4.Written)); err != nil {
				return err
			}
			continue
		}
		// Caddy may log one failed request both as an error and as an access event.
		if strings.HasPrefix(event.Logger, "http.log.error") {
			continue
		}
		if event.Status < 100 || event.Status > 599 {
			continue
		}
		listenerID := strings.TrimPrefix(event.Logger, "http.log.access.viya_access_")
		id := int64(0)
		if listenerID != event.Logger {
			id = matchLogSite(event.Request.Host, sitesByListener[listenerID])
		} else {
			id = matchLogSite(event.Request.Host, sites)
		}
		if id == 0 {
			continue
		}
		if event.Upload < 0 {
			event.Upload = 0
		}
		if event.Download < 0 {
			event.Download = 0
		}
		if parsed, e := url.Parse(event.Request.URI); e == nil {
			event.Request.URI = parsed.EscapedPath()
		} else {
			event.Request.URI = "/"
		}
		hour := int64(event.Time) / 3600 * 3600
		failures := 0
		if event.Status >= 400 {
			failures = 1
		}
		_, err = tx.ExecContext(a.ctx, `INSERT INTO traffic_hours(site_id,hour,requests,errors,upload,download) VALUES(?,?,1,?,?,?) ON CONFLICT(site_id,hour) DO UPDATE SET requests=requests+1,errors=errors+excluded.errors,upload=upload+excluded.upload,download=download+excluded.download`, id, hour, failures, event.Upload, event.Download)
		if err != nil {
			return err
		}
		data, _ := json.Marshal(event)
		if _, err = tx.ExecContext(a.ctx, `INSERT INTO site_access_events(site_id,event) VALUES(?,?)`, id, string(data)); err != nil {
			return err
		}
	}
	for _, id := range allIDs {
		if _, err = tx.ExecContext(a.ctx, `DELETE FROM site_access_events WHERE site_id=? AND id NOT IN (SELECT id FROM site_access_events WHERE site_id=? ORDER BY id DESC LIMIT ?)`, id, id, eventLimit); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(a.ctx, `INSERT INTO traffic_cursors(generation,offset,complete) VALUES(?,?,?) ON CONFLICT(generation) DO UPDATE SET offset=excluded.offset,complete=excluded.complete`, generation, offset, complete); err != nil {
		return err
	}
	if _, err = tx.ExecContext(a.ctx, `INSERT INTO traffic_files(path,size,mtime,offset,complete) VALUES(?,?,?,?,?) ON CONFLICT(path) DO UPDATE SET size=excluded.size,mtime=excluded.mtime,offset=excluded.offset,complete=excluded.complete`, path, info.Size(), info.ModTime().UnixNano(), offset, complete); err != nil {
		return err
	}
	return tx.Commit()
}
func (a *App) siteTraffic(w http.ResponseWriter, r *http.Request) {
	// hourly keeps the 24-hour shape the overview already renders; 7d and 30d
	// aggregate by day. traffic_hours retains 90 days, so 30d is the ceiling.
	window := strings.TrimSpace(r.URL.Query().Get("range"))
	hours := 24
	switch window {
	case "7d":
		hours = 24 * 7
	case "30d":
		hours = 24 * 30
	default:
		window = "24h"
	}
	cutoff := time.Now().Unix()/3600*3600 - int64(hours-1)*3600
	out := map[int64]*trafficSummary{}
	summaryFor := func(id int64) *trafficSummary {
		s := out[id]
		if s == nil {
			s = &trafficSummary{Hourly: []trafficPoint{}}
			out[id] = s
		}
		return s
	}
	// Cumulative totals are aggregated in SQLite, so the per-hour table only
	// has to be scanned for the 24-hour window the panel actually draws.
	totals, err := a.db.QueryContext(r.Context(), `SELECT site_id,SUM(requests),SUM(errors),SUM(upload),SUM(download) FROM traffic_hours GROUP BY site_id`)
	if err != nil {
		fail(w, 500, errors.New("could not read traffic statistics"))
		return
	}
	for totals.Next() {
		var id int64
		var counters trafficCounters
		if err = totals.Scan(&id, &counters.Requests, &counters.Errors, &counters.Upload, &counters.Download); err != nil {
			totals.Close()
			fail(w, 500, errors.New("could not read traffic statistics"))
			return
		}
		summaryFor(id).Total = counters
	}
	err = totals.Err()
	totals.Close()
	if err != nil {
		fail(w, 500, errors.New("could not read traffic statistics"))
		return
	}
	if window == "24h" {
		rows, err := a.db.QueryContext(r.Context(), `SELECT site_id,hour,requests,errors,upload,download FROM traffic_hours WHERE hour>=? ORDER BY hour`, cutoff)
		if err != nil {
			fail(w, 500, errors.New("could not read traffic statistics"))
			return
		}
		defer rows.Close()
		for rows.Next() {
			var id int64
			var p trafficPoint
			if err = rows.Scan(&id, &p.Hour, &p.Requests, &p.Errors, &p.Upload, &p.Download); err != nil {
				fail(w, 500, err)
				return
			}
			s := summaryFor(id)
			addTraffic(&s.Recent, p.trafficCounters)
			s.Hourly = append(s.Hourly, p)
		}
		if err = rows.Err(); err != nil {
			fail(w, 500, err)
			return
		}
		writeJSON(w, 200, out)
		return
	}
	rows, err := a.db.QueryContext(r.Context(), `SELECT site_id,hour-hour%86400 AS day,SUM(requests),SUM(errors),SUM(upload),SUM(download) FROM traffic_hours WHERE hour>=? GROUP BY site_id,day ORDER BY day`, cutoff)
	if err != nil {
		fail(w, 500, errors.New("could not read traffic statistics"))
		return
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var d trafficDay
		if err = rows.Scan(&id, &d.Day, &d.Requests, &d.Errors, &d.Upload, &d.Download); err != nil {
			fail(w, 500, err)
			return
		}
		s := summaryFor(id)
		addTraffic(&s.Recent, d.trafficCounters)
		s.Daily = append(s.Daily, d)
	}
	if err = rows.Err(); err != nil {
		fail(w, 500, err)
		return
	}
	writeJSON(w, 200, out)
}
func addTraffic(to *trafficCounters, from trafficCounters) {
	to.Requests += from.Requests
	to.Errors += from.Errors
	to.Upload += from.Upload
	to.Download += from.Download
}

// l4Traffic mirrors siteTraffic for layer4 rules. "requests" carries the
// connection count so the panel can reuse the same rendering for both.
func (a *App) l4Traffic(w http.ResponseWriter, r *http.Request) {
	window := strings.TrimSpace(r.URL.Query().Get("range"))
	hours := 24
	switch window {
	case "7d":
		hours = 24 * 7
	case "30d":
		hours = 24 * 30
	default:
		window = "24h"
	}
	cutoff := time.Now().Unix()/3600*3600 - int64(hours-1)*3600
	out := map[int64]*trafficSummary{}
	summaryFor := func(id int64) *trafficSummary {
		s := out[id]
		if s == nil {
			s = &trafficSummary{Hourly: []trafficPoint{}}
			out[id] = s
		}
		return s
	}
	totals, err := a.db.QueryContext(r.Context(), `SELECT rule_id,SUM(connections),SUM(upload),SUM(download) FROM layer4_traffic_hours GROUP BY rule_id`)
	if err != nil {
		fail(w, 500, errors.New("could not read traffic statistics"))
		return
	}
	for totals.Next() {
		var id int64
		var c trafficCounters
		if err = totals.Scan(&id, &c.Requests, &c.Upload, &c.Download); err != nil {
			totals.Close()
			fail(w, 500, errors.New("could not read traffic statistics"))
			return
		}
		summaryFor(id).Total = c
	}
	err = totals.Err()
	totals.Close()
	if err != nil {
		fail(w, 500, errors.New("could not read traffic statistics"))
		return
	}
	if window == "24h" {
		rows, err := a.db.QueryContext(r.Context(), `SELECT rule_id,hour,connections,upload,download FROM layer4_traffic_hours WHERE hour>=? ORDER BY hour`, cutoff)
		if err != nil {
			fail(w, 500, errors.New("could not read traffic statistics"))
			return
		}
		defer rows.Close()
		for rows.Next() {
			var id int64
			var p trafficPoint
			if err = rows.Scan(&id, &p.Hour, &p.Requests, &p.Upload, &p.Download); err != nil {
				fail(w, 500, err)
				return
			}
			s := summaryFor(id)
			addTraffic(&s.Recent, p.trafficCounters)
			s.Hourly = append(s.Hourly, p)
		}
		if err = rows.Err(); err != nil {
			fail(w, 500, err)
			return
		}
		writeJSON(w, 200, out)
		return
	}
	rows, err := a.db.QueryContext(r.Context(), `SELECT rule_id,hour-hour%86400 AS day,SUM(connections),SUM(upload),SUM(download) FROM layer4_traffic_hours WHERE hour>=? GROUP BY rule_id,day ORDER BY day`, cutoff)
	if err != nil {
		fail(w, 500, errors.New("could not read traffic statistics"))
		return
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var d trafficDay
		if err = rows.Scan(&id, &d.Day, &d.Requests, &d.Upload, &d.Download); err != nil {
			fail(w, 500, err)
			return
		}
		s := summaryFor(id)
		addTraffic(&s.Recent, d.trafficCounters)
		s.Daily = append(s.Daily, d)
	}
	if err = rows.Err(); err != nil {
		fail(w, 500, err)
		return
	}
	writeJSON(w, 200, out)
}
func (a *App) siteAccessLogs(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		fail(w, 400, errors.New("invalid site id"))
		return
	}
	rows, err := a.db.QueryContext(r.Context(), `SELECT event FROM site_access_events WHERE site_id=? ORDER BY id DESC LIMIT 50`, id)
	if err != nil {
		fail(w, 500, errors.New("could not read site access logs"))
		return
	}
	defer rows.Close()
	out := []json.RawMessage{}
	for rows.Next() {
		var event string
		if err = rows.Scan(&event); err != nil {
			fail(w, 500, err)
			return
		}
		out = append(out, json.RawMessage(event))
	}
	if err = rows.Err(); err != nil {
		fail(w, 500, err)
		return
	}
	writeJSON(w, 200, out)
}
