package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/mem"
)

//go:embed cn-ranges.txt
var chinaRangesRaw string

func chinaAndPrivateRanges() []string {
	ranges := []string{"127.0.0.0/8", "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "100.64.0.0/10", "169.254.0.0/16", "::1/128", "fc00::/7", "fe80::/10"}
	for _, line := range strings.Split(chinaRangesRaw, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			ranges = append(ranges, line)
		}
	}
	return ranges
}

var metricsCache struct {
	sync.Mutex
	at       time.Time
	value    map[string]any
	sampling chan struct{}
}

func readMetricsCache(maxAge time.Duration) (map[string]any, bool) {
	metricsCache.Lock()
	defer metricsCache.Unlock()
	if metricsCache.value == nil {
		return nil, false
	}
	if maxAge > 0 && time.Since(metricsCache.at) >= maxAge {
		return nil, false
	}
	return metricsCache.value, true
}

// claimMetricsSample elects a single sampler; concurrent callers receive the
// channel that closes when the sample is stored. The 300 ms CPU probe runs
// outside the cache lock so extra browser tabs no longer queue behind it.
func claimMetricsSample() (chan struct{}, bool) {
	metricsCache.Lock()
	defer metricsCache.Unlock()
	if metricsCache.sampling != nil {
		return metricsCache.sampling, false
	}
	metricsCache.sampling = make(chan struct{})
	return metricsCache.sampling, true
}

func storeMetrics(value map[string]any, done chan struct{}) {
	metricsCache.Lock()
	metricsCache.value = value
	metricsCache.at = time.Now()
	metricsCache.sampling = nil
	metricsCache.Unlock()
	close(done)
}

func (a *App) systemMetrics(w http.ResponseWriter, r *http.Request) {
	if value, ok := readMetricsCache(3 * time.Second); ok {
		writeJSON(w, 200, value)
		return
	}
	wait, mine := claimMetricsSample()
	if mine {
		value := a.collectMetrics(r)
		storeMetrics(value, wait)
		writeJSON(w, 200, value)
		return
	}
	select {
	case <-wait:
	case <-r.Context().Done():
		return
	case <-time.After(5 * time.Second):
	}
	if value, ok := readMetricsCache(0); ok {
		writeJSON(w, 200, value)
		return
	}
	fail(w, http.StatusServiceUnavailable, errors.New("系统指标暂不可用，请稍后重试"))
}

func (a *App) collectMetrics(r *http.Request) map[string]any {
	ctx, cancel := context.WithTimeout(r.Context(), 4*time.Second)
	defer cancel()
	result := map[string]any{"sampled_at": time.Now().Unix(), "platform": runtime.GOOS, "cores": runtime.NumCPU()}
	errs := []string{}
	values, err := cpu.PercentWithContext(ctx, 300*time.Millisecond, false)
	if err == nil && len(values) > 0 {
		result["cpu_percent"] = values[0]
	} else {
		errs = append(errs, "CPU unavailable")
	}
	m, err := mem.VirtualMemoryWithContext(ctx)
	if err == nil {
		result["memory"] = map[string]any{"percent": m.UsedPercent, "used": m.Used, "total": m.Total}
	} else {
		errs = append(errs, "Memory unavailable")
	}
	path, err := filepath.Abs(a.dataDir)
	if err == nil {
		d, e := disk.UsageWithContext(ctx, path)
		if e == nil {
			result["disk"] = map[string]any{"percent": d.UsedPercent, "used": d.Used, "total": d.Total, "path": path}
		} else {
			errs = append(errs, "Disk unavailable")
		}
	}
	if u, e := host.UptimeWithContext(ctx); e == nil {
		result["uptime"] = u
	}
	result["errors"] = errs
	return result
}
func (a *App) toggleHostEnabled(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Enabled *bool `json:"enabled"`
		Version int64 `json:"version"`
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 || json.NewDecoder(http.MaxBytesReader(w, r.Body, 2048)).Decode(&input) != nil || input.Enabled == nil {
		fail(w, 400, errors.New("Invalid site state"))
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	res, err := a.db.ExecContext(r.Context(), `UPDATE proxy_hosts SET enabled=?,version=version+1,updated_at=CURRENT_TIMESTAMP WHERE id=? AND version=?`, boolInt(*input.Enabled), id, input.Version)
	if err != nil {
		fail(w, 500, err)
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		fail(w, 409, errors.New("Site changed; refresh and retry"))
		return
	}
	if err = a.applyLocked(r.Context()); err != nil {
		fail(w, 502, errors.New("State saved; engine apply failed. Retry applying configuration"))
		return
	}
	writeJSON(w, 200, map[string]any{"enabled": *input.Enabled, "version": input.Version + 1})
}
