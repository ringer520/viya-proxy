package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
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

// diskAlertPercent is the data-directory usage that triggers a notification.
const diskAlertPercent = 90

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

// The distro/kernel banner never changes while the process runs, so read it
// once and keep it. A failed read is retried on the next sample instead of
// being latched off, which a sync.Once would do.
var (
	hostInfoMu sync.Mutex
	hostInfo   map[string]any
)

func staticHostInfo() map[string]any {
	hostInfoMu.Lock()
	defer hostInfoMu.Unlock()
	if hostInfo != nil {
		return hostInfo
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	info, err := host.InfoWithContext(ctx)
	if err != nil {
		return nil
	}
	// Distro name/version come from the host's own release files, so a
	// container reports the image it runs on rather than the hypervisor.
	hostInfo = map[string]any{
		"hostname":         info.Hostname,
		"platform":         info.Platform,
		"platform_version": info.PlatformVersion,
		"kernel":           info.KernelVersion,
		"arch":             info.KernelArch,
	}
	return hostInfo
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
			// Alerting on the data directory matters because it holds the
			// database and the certificate store; a full disk there means
			// every write, including renewals, starts failing.
			if d.UsedPercent >= diskAlertPercent {
				a.notify("disk_full", "数据目录磁盘接近写满",
					fmt.Sprintf("%s 已使用 %.0f%%（剩余 %.1f GiB）。数据库与证书存储位于此处，请尽快清理。",
						path, d.UsedPercent, float64(d.Free)/1024/1024/1024))
			}
		} else {
			errs = append(errs, "Disk unavailable")
		}
	}
	if info := staticHostInfo(); info != nil {
		result["host"] = info
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
	var previous int
	if err := a.db.QueryRowContext(r.Context(), `SELECT enabled FROM proxy_hosts WHERE id=?`, id).Scan(&previous); err != nil {
		fail(w, 404, errors.New("Site not found"))
		return
	}
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
		// Roll the enabled flag back so the panel does not show a state that
		// the engine never applied.
		rollbackCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		if _, rbErr := a.db.ExecContext(rollbackCtx, `UPDATE proxy_hosts SET enabled=?,version=version+1 WHERE id=?`, previous, id); rbErr != nil {
			slog.Error("rollback site enabled state", "id", id, "error", rbErr)
			cancel()
			fail(w, 500, errors.New("Site apply failed and the previous state could not be restored"))
			return
		}
		if rbErr := a.applyLocked(rollbackCtx); rbErr != nil {
			slog.Error("reapply after site rollback", "error", rbErr)
		}
		cancel()
		fail(w, 502, errors.New("Site state could not be applied; previous state restored"))
		return
	}
	writeJSON(w, 200, map[string]any{"enabled": *input.Enabled, "version": input.Version + 1})
}
