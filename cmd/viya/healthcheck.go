package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Upstream health gives the overview a truthful answer to "is this site
// actually working?" without probing through the public hostname: it dials the
// upstream the proxy would dial, which is what fails when a container is down.
const (
	upstreamProbeInterval = 60 * time.Second
	upstreamProbeTimeout  = 3 * time.Second
	// upstreamAlertAfter is how many consecutive failures raise one alert, so a
	// single dropped packet does not page anybody.
	upstreamAlertAfter  = 3
	upstreamProbeFanout = 4
)

type upstreamHealth struct {
	OK        bool    `json:"ok"`
	LatencyMS float64 `json:"latency_ms"`
	CheckedAt int64   `json:"checked_at"`
	Target    string  `json:"target"`
	Error     string  `json:"error,omitempty"`
	Failures  int     `json:"failures"`
}

// upstreamDialTarget normalises a configured upstream into host:port.
func upstreamDialTarget(upstream string) string {
	raw := strings.TrimSpace(upstream)
	if raw == "" {
		return ""
	}
	if strings.Contains(raw, "://") {
		parsed, err := url.Parse(raw)
		if err != nil || parsed.Host == "" {
			return ""
		}
		if parsed.Port() != "" {
			return parsed.Host
		}
		port := "80"
		if parsed.Scheme == "https" {
			port = "443"
		}
		return net.JoinHostPort(parsed.Hostname(), port)
	}
	if index := strings.IndexAny(raw, "/?#"); index >= 0 {
		raw = raw[:index]
	}
	if _, _, err := net.SplitHostPort(raw); err == nil {
		return raw
	}
	return net.JoinHostPort(raw, "80")
}

func (a *App) probeUpstreams(ctx context.Context) {
	if a == nil || a.db == nil || ctx.Err() != nil {
		return
	}
	rows, err := a.db.QueryContext(ctx, `SELECT id,domain,upstream FROM proxy_hosts WHERE enabled=1`)
	if err != nil {
		return
	}
	type target struct {
		id       int64
		domain   string
		upstream string
	}
	targets := make([]target, 0, 16)
	for rows.Next() {
		var item target
		if rows.Scan(&item.id, &item.domain, &item.upstream) == nil {
			targets = append(targets, item)
		}
	}
	rows.Close()

	results := make([]struct {
		id     int64
		domain string
		health upstreamHealth
	}, len(targets))
	slots := make(chan struct{}, upstreamProbeFanout)
	var workers sync.WaitGroup
	for index, item := range targets {
		slots <- struct{}{}
		workers.Add(1)
		go func(index int, item target) {
			defer workers.Done()
			defer func() { <-slots }()
			address := upstreamDialTarget(item.upstream)
			health := upstreamHealth{CheckedAt: time.Now().Unix(), Target: address}
			if address == "" {
				health.Error = "上游地址无法解析"
			} else {
				probeCtx, cancel := context.WithTimeout(ctx, upstreamProbeTimeout)
				started := time.Now()
				conn, dialErr := (&net.Dialer{}).DialContext(probeCtx, "tcp", address)
				cancel()
				if conn != nil {
					conn.Close()
				}
				health.LatencyMS = float64(time.Since(started).Microseconds()) / 1000
				health.OK = dialErr == nil
				if dialErr != nil {
					health.Error = dialErr.Error()
				}
			}
			results[index].id = item.id
			results[index].domain = item.domain
			results[index].health = health
		}(index, item)
	}
	workers.Wait()
	if ctx.Err() != nil {
		return
	}

	a.healthMu.Lock()
	if a.health == nil {
		a.health = make(map[int64]upstreamHealth)
	}
	type transition struct {
		domain string
		down   bool
		health upstreamHealth
	}
	changes := make([]transition, 0, 2)
	seen := make(map[int64]bool, len(results))
	for _, result := range results {
		seen[result.id] = true
		previous, existed := a.health[result.id]
		if result.health.OK {
			result.health.Failures = 0
		} else {
			result.health.Failures = previous.Failures + 1
			if !existed {
				// A site that has never been reachable starts at 1 so a first
				// failed probe still has to repeat before alerting.
				result.health.Failures = 1
			}
		}
		a.health[result.id] = result.health
		if result.health.OK && existed && !previous.OK && previous.Failures >= upstreamAlertAfter {
			changes = append(changes, transition{domain: result.domain, down: false, health: result.health})
		}
		if !result.health.OK && result.health.Failures == upstreamAlertAfter {
			changes = append(changes, transition{domain: result.domain, down: true, health: result.health})
		}
	}
	for id := range a.health {
		if !seen[id] {
			delete(a.health, id)
		}
	}
	a.healthMu.Unlock()

	for _, change := range changes {
		if change.down {
			a.notify("upstream_down:"+change.domain, "站点上游无响应",
				fmt.Sprintf("%s 的上游 %s 连续 %d 次连接失败：%s", change.domain, change.health.Target, change.health.Failures, change.health.Error))
			continue
		}
		if a.loadAlertConfig().NotifyRecovery {
			a.notify("upstream_up:"+change.domain, "站点上游已恢复",
				fmt.Sprintf("%s 的上游 %s 已恢复响应，延迟 %.0f ms。", change.domain, change.health.Target, change.health.LatencyMS))
		}
	}
}

func (a *App) healthWorker() {
	a.probeUpstreams(a.ctx)
	ticker := time.NewTicker(upstreamProbeInterval)
	defer ticker.Stop()
	for {
		select {
		case <-a.ctx.Done():
			return
		case <-ticker.C:
			a.probeUpstreams(a.ctx)
		}
	}
}

func (a *App) upstreamHealthReport(w http.ResponseWriter, r *http.Request) {
	a.healthMu.RLock()
	report := make(map[string]upstreamHealth, len(a.health))
	for id, health := range a.health {
		report[fmt.Sprint(id)] = health
	}
	a.healthMu.RUnlock()
	writeJSON(w, http.StatusOK, report)
}

// forceProbeUpstreams lets the UI refresh the report on demand instead of
// waiting for the next tick.
func (a *App) forceProbeUpstreams(w http.ResponseWriter, r *http.Request) {
	a.probeUpstreams(r.Context())
	a.upstreamHealthReport(w, r)
}
