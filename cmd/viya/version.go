package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// version is stamped at build time:
//
//	go build -ldflags "-X main.version=v1.2.3"
//
// Unreleased builds keep the "dev" marker so the update button stays hidden.
var version = "dev"
var buildCommit = "unknown"
var buildTime = "unknown"
var buildSource = "unknown"

const (
	updateRepoOwner = "ringer520"
	updateRepoName  = "viya-proxy"
	// Releases are immutable, so the "latest" lookup is cached for a while to
	// stay well inside GitHub's unauthenticated API budget.
	updateCacheTTL = 10 * time.Minute
)

type releaseInfo struct {
	Tag     string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
	Body    string `json:"body"`
}

type versionInfo struct {
	Commit          string `json:"commit"`
	BuiltAt         string `json:"built_at"`
	Source          string `json:"source"`
	Schema          int    `json:"schema_version"`
	Current         string `json:"current"`
	Latest          string `json:"latest,omitempty"`
	UpdateAvailable bool   `json:"update_available"`
	ReleaseURL      string `json:"release_url,omitempty"`
	Notes           string `json:"notes,omitempty"`
	InDocker        bool   `json:"in_docker"`
	Managed         bool   `json:"managed"` // able to self-replace (Linux binary install)
	CheckedAt       string `json:"checked_at,omitempty"`
	Error           string `json:"error,omitempty"`
}

var (
	updateCacheMu      sync.Mutex
	updateRunMu        sync.Mutex
	updateCacheRelease *releaseInfo
	updateCacheAt      time.Time
	updateProgressMu   sync.RWMutex
	updateProgress     = versionUpdateProgress{Stage: "idle", Percent: 0}
)

type versionUpdateProgress struct {
	Active  bool   `json:"active"`
	Stage   string `json:"stage"`
	Percent int    `json:"percent"`
	Error   string `json:"error,omitempty"`
	Warning string `json:"warning,omitempty"`
}

// engineSwapWarning is a fixed, path-free message: the detailed error only
// goes to the program log, never into an API response a viewer can read.
const engineSwapWarning = "主程序已更新，但 Caddy 引擎替换失败，当前仍在运行旧版引擎。请检查引擎路径与目录权限后手动替换并重启。"

func setVersionUpdateProgress(stage string, percent int, err error) {
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}
	updateProgressMu.Lock()
	// A non-fatal engine warning must survive the later "restarting" stages.
	warning := updateProgress.Warning
	updateProgress = versionUpdateProgress{Stage: stage, Percent: percent, Active: percent > 0 && percent < 100 && err == nil, Warning: warning}
	if err != nil {
		updateProgress.Error = err.Error()
	}
	updateProgressMu.Unlock()
}

func setVersionUpdateWarning(warning string) {
	updateProgressMu.Lock()
	updateProgress.Warning = warning
	updateProgressMu.Unlock()
}

func currentUpdateWarning() string {
	updateProgressMu.RLock()
	defer updateProgressMu.RUnlock()
	return updateProgress.Warning
}

// swapEngineBinary installs the new Caddy binary. Unlike the main binary this
// is best-effort: the panel restarts fine on the new build while the old engine
// keeps serving, so a failure must be reported instead of silently discarded.
// Returning a warning (rather than an error) keeps the update itself successful
// while making the mismatch visible in the log, progress API and UI.
func swapEngineBinaryWith(path string, install func() error) string {
	if err := install(); err != nil {
		slog.Warn("self-update: engine binary not replaced", "path", path, "error", err, "hint", engineSwapWarning)
		return engineSwapWarning
	}
	return ""
}

func swapEngineBinary(path string, content []byte) string {
	return swapEngineBinaryWith(path, func() error {
		return replaceExecutable(path, content)
	})
}

func (a *App) versionUpdateProgressHandler(w http.ResponseWriter, r *http.Request) {
	updateProgressMu.RLock()
	progress := updateProgress
	updateProgressMu.RUnlock()
	writeJSON(w, http.StatusOK, progress)
}

func isDockerized() bool {
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return true
	}
	if data, err := os.ReadFile("/proc/1/cgroup"); err == nil {
		s := string(data)
		if strings.Contains(s, "docker") || strings.Contains(s, "containerd") || strings.Contains(s, "kubepods") {
			return true
		}
	}
	return false
}

// canSelfUpdate reports whether the process is able to swap its own binary on
// disk and come back up. That is the Linux binary install (systemd/procd both
// respawn the process); containers should pull a fresh image instead.
func canSelfUpdate() bool {
	if runtime.GOOS != "linux" {
		return false
	}
	return !isDockerized()
}

func fetchLatestRelease(ctxTimeout time.Duration) (*releaseInfo, error) {
	updateCacheMu.Lock()
	defer updateCacheMu.Unlock()
	if updateCacheRelease != nil && time.Since(updateCacheAt) < updateCacheTTL {
		return updateCacheRelease, nil
	}
	client := &http.Client{Timeout: ctxTimeout}
	req, err := http.NewRequest("GET", fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", updateRepoOwner, updateRepoName), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", updateRepoName)
	resp, err := client.Do(req)
	if err != nil {
		return nil, errors.New("无法连接 GitHub，请检查服务器网络")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("GitHub 返回状态 %d", resp.StatusCode)
	}
	var rel releaseInfo
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&rel); err != nil {
		return nil, errors.New("无法解析 GitHub 响应")
	}
	if rel.Tag == "" {
		return nil, errors.New("GitHub 未返回版本信息")
	}
	updateCacheRelease = &rel
	updateCacheAt = time.Now()
	return &rel, nil
}

// versionNewer reports whether candidate (e.g. "v0.2.0") is strictly newer
// than current ("v0.1.0"). Pre-release suffixes (e.g. "-rc1") make a version
// older than its plain release.
func versionNewer(current, candidate string) bool {
	type parsed struct {
		nums      []int
		preSuffix string
	}
	parse := func(s string) parsed {
		s = strings.TrimPrefix(strings.TrimSpace(s), "v")
		pre := ""
		if i := strings.IndexAny(s, "-+"); i >= 0 {
			pre = s[i+1:]
			s = s[:i]
		}
		parts := strings.Split(s, ".")
		nums := make([]int, 0, len(parts))
		for _, p := range parts {
			n, err := strconv.Atoi(p)
			if err != nil {
				break
			}
			nums = append(nums, n)
		}
		return parsed{nums: nums, preSuffix: pre}
	}
	a, b := parse(current), parse(candidate)
	for i := 0; i < len(a.nums) || i < len(b.nums); i++ {
		x, y := 0, 0
		if i < len(a.nums) {
			x = a.nums[i]
		}
		if i < len(b.nums) {
			y = b.nums[i]
		}
		if x != y {
			return y > x
		}
	}
	// Numeric cores are equal; a plain release is newer than a pre-release.
	if a.preSuffix == b.preSuffix {
		return false
	}
	// Current is a pre-release and candidate is a plain release → candidate is newer.
	return a.preSuffix != "" && b.preSuffix == ""
}

func (a *App) versionInfoHandler(w http.ResponseWriter, r *http.Request) {
	info := versionInfo{
		Commit: buildCommit, BuiltAt: buildTime, Source: buildSource, Schema: schemaVersion,
		Current:  version,
		InDocker: isDockerized(),
		Managed:  canSelfUpdate(),
	}
	if r.URL.Query().Get("local") == "1" {
		writeJSON(w, 200, info)
		return
	}
	rel, err := fetchLatestRelease(8 * time.Second)
	if err != nil {
		info.Error = err.Error()
		writeJSON(w, 200, info)
		return
	}
	info.Latest = rel.Tag
	info.ReleaseURL = rel.HTMLURL
	info.Notes = rel.Body
	info.CheckedAt = time.Now().UTC().Format(time.RFC3339)
	info.UpdateAvailable = versionNewer(version, rel.Tag) && info.Managed
	writeJSON(w, 200, info)
}

func releaseDownloadBases(tag string) []string {
	direct := fmt.Sprintf("https://github.com/%s/%s/releases/download/%s", updateRepoOwner, updateRepoName, tag)
	var candidates []string
	if proxy := strings.TrimSpace(os.Getenv("GH_PROXY")); proxy != "" {
		if !strings.HasSuffix(proxy, "/") {
			proxy += "/"
		}
		candidates = append(candidates, proxy+direct)
	}
	candidates = append(candidates,
		direct,
		"https://ghfast.top/"+direct,
		"https://ghproxy.net/"+direct,
		"https://gh-proxy.com/"+direct,
	)
	var bases []string
	seen := make(map[string]bool)
	for _, c := range candidates {
		if !seen[c] {
			seen[c] = true
			bases = append(bases, c)
		}
	}
	return bases
}

func parseAssetChecksum(sums []byte, asset string) (string, bool) {
	for _, line := range strings.Split(string(sums), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || !strings.HasSuffix(fields[1], asset) {
			continue
		}
		sum := strings.ToLower(fields[0])
		if len(sum) != 64 {
			continue
		}
		if _, err := hex.DecodeString(sum); err == nil {
			return sum, true
		}
	}
	return "", false
}

type mirrorProbeResult struct {
	base     string
	duration time.Duration
	sum      string
	valid    bool
	err      error
}

func probeSingleMirror(ctx context.Context, base, asset string) (string, time.Duration, bool, error) {
	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/sha256sums.txt", nil)
	if err != nil {
		return "", 0, false, err
	}
	req.Header.Set("User-Agent", updateRepoName)
	client := &http.Client{
		Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			ResponseHeaderTimeout: 3 * time.Second,
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", 0, false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", 0, false, fmt.Errorf("status %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", 0, false, err
	}
	duration := time.Since(start)
	sum, ok := parseAssetChecksum(data, asset)
	hasFormat := strings.Contains(string(data), ".tar.gz")
	if !ok {
		return "", duration, hasFormat, errors.New("checksum not found")
	}
	return sum, duration, true, nil
}

// pickChecksum returns the checksum the most mirrors agree on. Ties break on
// the lexicographically smaller value so the result is deterministic.
func pickChecksum(votes map[string]int) (string, int) {
	best, bestCount := "", 0
	for sum, count := range votes {
		if count > bestCount || (count == bestCount && sum < best) {
			best, bestCount = sum, count
		}
	}
	return best, bestCount
}

func probeReleaseMirrors(bases []string, asset string, totalTimeout time.Duration) (orderedBases []string, wantSum string, gotValidFormat bool, err error) {
	if len(bases) == 0 {
		return nil, "", false, errors.New("无法下载校验文件")
	}

	ctx, cancel := context.WithTimeout(context.Background(), totalTimeout)
	defer cancel()

	type resWithBase struct {
		base string
		res  mirrorProbeResult
	}

	ch := make(chan resWithBase, len(bases))
	for _, b := range bases {
		go func(base string) {
			sum, dur, valid, probeErr := probeSingleMirror(ctx, base, asset)
			ch <- resWithBase{
				base: base,
				res: mirrorProbeResult{
					base:     base,
					duration: dur,
					sum:      sum,
					valid:    valid,
					err:      probeErr,
				},
			}
		}(b)
	}

	var successes []mirrorProbeResult
	seenSuccess := make(map[string]bool)
	// Every mirror that answered contributes its published checksum; the
	// winner is decided by majority below instead of by arrival order.
	sumVotes := map[string]int{}
	completed := 0
	var graceTimer *time.Timer
	var graceCh <-chan time.Time

	for completed < len(bases) {
		select {
		case r := <-ch:
			completed++
			if r.res.valid {
				gotValidFormat = true
			}
			if r.res.err == nil && r.res.sum != "" {
				sumVotes[r.res.sum]++
				if graceCh == nil {
					// After the fastest mirror responds, wait a brief grace period (350ms)
					// to collect other close contenders so we can rank them.
					graceTimer = time.NewTimer(350 * time.Millisecond)
					defer graceTimer.Stop()
					graceCh = graceTimer.C
				}
				if !seenSuccess[r.base] {
					seenSuccess[r.base] = true
					successes = append(successes, r.res)
				}
			}
		case <-graceCh:
			completed = len(bases)
		case <-ctx.Done():
			completed = len(bases)
		}
	}

	if len(successes) == 0 {
		if gotValidFormat {
			return nil, "", true, errors.New("校验文件中找不到当前架构的产物")
		}
		return nil, "", false, errors.New("无法下载校验文件")
	}

	// Mirrors can disagree (stale cache, or a captive-portal page that still
	// parses as a checksum file). Trust the value most mirrors publish rather
	// than whichever one answered first, so a single bad mirror cannot make
	// every download fail verification.
	if len(sumVotes) > 0 {
		best, bestCount := pickChecksum(sumVotes)
		if len(sumVotes) > 1 {
			slog.Warn("self-update: mirrors published different checksums",
				"distinct", len(sumVotes), "chosen", best, "agreeing", bestCount, "mirrors", len(successes))
		}
		wantSum = best
	}

	// Sort successes by duration ascending (fastest first)
	slices.SortFunc(successes, func(a, b mirrorProbeResult) int {
		if a.duration < b.duration {
			return -1
		} else if a.duration > b.duration {
			return 1
		}
		return 0
	})

	for _, s := range successes {
		orderedBases = append(orderedBases, s.base)
	}
	// Append remaining bases as last-resort fallbacks
	for _, b := range bases {
		if !seenSuccess[b] {
			orderedBases = append(orderedBases, b)
		}
	}

	return orderedBases, wantSum, gotValidFormat, nil
}

// downloadAndSwap fetches the release archive for this machine's architecture,
// verifies it against the published sha256sums, and atomically replaces the
// running Viya binary plus its bundled Caddy using bounded temp files on disk
// (avoiding /tmp tmpfs on low-RAM routers).
func (a *App) downloadAndSwap(tag string) error {
	setVersionUpdateProgress("准备校验", 2, nil)
	arch := runtime.GOARCH
	if arch == "arm" {
		arch = "armv7"
	}
	asset := fmt.Sprintf("viya-linux-%s.tar.gz", arch)
	bases := releaseDownloadBases(tag)

	orderedBases, wantSum, gotValidFormat, probeErr := probeReleaseMirrors(bases, asset, 4*time.Second)
	if probeErr != nil {
		if gotValidFormat {
			return errors.New("校验文件中找不到当前架构的产物")
		}
		return errors.New("无法下载校验文件")
	}

	slog.Info("self-update: mirror probe completed", "fastest", orderedBases[0], "candidates", len(orderedBases))

	tempRoot := a.dataDir
	archiveFile, err := os.CreateTemp(tempRoot, "viya-release-*.tar.gz")
	if err != nil {
		return errors.New("无法创建更新临时文件")
	}
	archivePath := archiveFile.Name()
	_ = archiveFile.Close()
	defer os.Remove(archivePath)

	var verified bool
	var checksumMismatch bool
	for i, base := range orderedBases {
		hasFallback := i < len(orderedBases)-1
		setVersionUpdateProgress("正在下载更新包", 4, nil)
		opts := downloadMonitorOptions{
			minSpeedBytesPerSec: 150 * 1024, // 150 KB/s
			warmupDuration:      5 * time.Second,
			stallTimeout:        10 * time.Second,
			hasFallback:         hasFallback,
		}
		actualSum, dlErr := downloadToFileWithProgressAndSHA256(base+"/"+asset, archivePath, 256<<20, func(downloaded, total int64) {
			percent := 4
			if total > 0 {
				percent += int(float64(downloaded) / float64(total) * 82)
			}
			setVersionUpdateProgress("正在下载更新包", percent, nil)
		}, opts)
		if dlErr != nil {
			slog.Warn("self-update: mirror failed or speed too low, trying next mirror", "base", base, "error", dlErr)
			_ = os.Remove(archivePath)
			continue
		}
		if actualSum != wantSum {
			slog.Warn("self-update: checksum mismatch on mirror", "base", base)
			checksumMismatch = true
			_ = os.Remove(archivePath)
			continue
		}
		verified = true
		slog.Info("self-update: download verified successfully", "base", base)
		break
	}
	if !verified {
		if checksumMismatch {
			return errors.New("下载文件校验失败，已取消更新")
		}
		return errors.New("无法下载新版本，请检查服务器网络")
	}
	setVersionUpdateProgress("校验更新包", 88, nil)

	stageDir, err := os.MkdirTemp(tempRoot, "viya-extract-*")
	if err != nil {
		return errors.New("无法创建解压临时目录")
	}
	defer os.RemoveAll(stageDir)

	viyaStage, caddyStage, err := extractArchiveBinaries(archivePath, stageDir)
	if err != nil {
		return err
	}
	_ = os.Remove(archivePath)
	setVersionUpdateProgress("替换程序文件", 94, nil)

	selfPath, err := os.Executable()
	if err != nil {
		return errors.New("无法定位当前程序路径")
	}
	if err := replaceExecutableFromFile(selfPath, viyaStage); err != nil {
		return fmt.Errorf("无法替换主程序：%w", err)
	}
	caddyPath := a.caddyBin
	if caddyPath == "" {
		caddyPath = filepath.Join(filepath.Dir(selfPath), "caddy")
	}
	// Caddy is a subprocess, so a missing path is not fatal: the new binary
	// can still come up and the engine can be fixed later.
	if warning := swapEngineBinaryWith(caddyPath, func() error {
		return replaceExecutableFromFile(caddyPath, caddyStage)
	}); warning != "" {
		setVersionUpdateWarning(warning)
	}
	setVersionUpdateProgress("正在重启", 97, nil)
	return nil
}

func extractArchiveBinaries(archivePath, stageDir string) (viyaStage, caddyStage string, err error) {
	f, err := os.Open(archivePath)
	if err != nil {
		return "", "", errors.New("无法打开已下载的压缩包")
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return "", "", errors.New("无法解析新版本压缩包")
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, nextErr := tr.Next()
		if errors.Is(nextErr, io.EOF) {
			break
		}
		if nextErr != nil {
			return "", "", errors.New("无法读取新版本压缩包")
		}
		if hdr.Typeflag != tar.TypeReg || hdr.Size < 0 || hdr.Size > 256<<20 {
			continue
		}
		name := filepath.Base(hdr.Name)
		var destName string
		if strings.HasPrefix(name, "viya-linux-") {
			destName = "viya-new"
		} else if strings.HasPrefix(name, "caddy-linux-") {
			destName = "caddy-new"
		} else {
			continue
		}
		destPath := filepath.Join(stageDir, destName)
		out, openErr := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
		if openErr != nil {
			return "", "", errors.New("无法暂存新版本二进制")
		}
		_, copyErr := io.Copy(out, io.LimitReader(tr, hdr.Size))
		closeErr := out.Close()
		if copyErr != nil || closeErr != nil {
			return "", "", errors.New("无法读取新版本二进制")
		}
		if destName == "viya-new" {
			viyaStage = destPath
		} else {
			caddyStage = destPath
		}
	}
	if viyaStage == "" || caddyStage == "" {
		return "", "", errors.New("新版本压缩包缺少二进制文件")
	}
	return viyaStage, caddyStage, nil
}

type downloadMonitorOptions struct {
	minSpeedBytesPerSec int64         // e.g. 150 * 1024 (150 KB/s)
	warmupDuration      time.Duration // e.g. 5 * time.Second
	stallTimeout        time.Duration // e.g. 10 * time.Second
	hasFallback         bool
}

func downloadToFileWithProgressAndSHA256(
	url, destPath string,
	limit int64,
	progress func(downloaded, total int64),
	opts downloadMonitorOptions,
) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", updateRepoName)

	client := &http.Client{
		Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			ResponseHeaderTimeout: 15 * time.Second, // 首包响应超时：15秒无响应头则快速失败
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("status %d", resp.StatusCode)
	}
	out, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return "", err
	}
	defer out.Close()

	hasher := sha256.New()
	mw := io.MultiWriter(out, hasher)
	reader := io.LimitReader(resp.Body, limit+1)

	var (
		downloaded atomic.Int64
		abortedErr error
		abortMu    sync.Mutex
	)

	monitorStop := make(chan struct{})
	var monitorWg sync.WaitGroup
	monitorWg.Add(1)

	go func() {
		defer monitorWg.Done()
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()

		startTime := time.Now()
		lastProgressTime := startTime
		lastBytes := int64(0)

		const windowSec = 5
		var samples [windowSec]int64
		sampleIdx := 0
		sampleCount := 0

		for {
			select {
			case <-monitorStop:
				return
			case now := <-ticker.C:
				cur := downloaded.Load()
				delta := cur - lastBytes
				lastBytes = cur

				if delta > 0 {
					lastProgressTime = now
				} else if now.Sub(lastProgressTime) >= opts.stallTimeout {
					abortMu.Lock()
					abortedErr = fmt.Errorf("download stalled for %v", opts.stallTimeout)
					abortMu.Unlock()
					cancel()
					return
				}

				samples[sampleIdx%windowSec] = delta
				sampleIdx++
				if sampleCount < windowSec {
					sampleCount++
				}

				// 仅在有可用备用镜像、且经过预热期后触发低速主动熔断
				if opts.hasFallback && opts.minSpeedBytesPerSec > 0 &&
					now.Sub(startTime) >= opts.warmupDuration && sampleCount >= windowSec {
					var windowTotal int64
					for i := 0; i < windowSec; i++ {
						windowTotal += samples[i]
					}
					avgSpeed := windowTotal / int64(windowSec)
					if avgSpeed < opts.minSpeedBytesPerSec {
						abortMu.Lock()
						abortedErr = fmt.Errorf("download speed too low (%d KB/s < %d KB/s)", avgSpeed/1024, opts.minSpeedBytesPerSec/1024)
						abortMu.Unlock()
						cancel()
						return
					}
				}
			}
		}
	}()

	chunk := make([]byte, 64*1024)
	for {
		n, readErr := reader.Read(chunk)
		if n > 0 {
			downloaded.Add(int64(n))
			cur := downloaded.Load()
			if cur > limit {
				close(monitorStop)
				monitorWg.Wait()
				return "", errors.New("下载内容超过大小限制")
			}
			if _, writeErr := mw.Write(chunk[:n]); writeErr != nil {
				close(monitorStop)
				monitorWg.Wait()
				return "", writeErr
			}
			if progress != nil {
				progress(cur, resp.ContentLength)
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			close(monitorStop)
			monitorWg.Wait()
			abortMu.Lock()
			customErr := abortedErr
			abortMu.Unlock()
			if customErr != nil {
				return "", customErr
			}
			return "", readErr
		}
	}

	close(monitorStop)
	monitorWg.Wait()

	if err := out.Close(); err != nil {
		return "", err
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func replaceExecutableFromFile(target, srcPath string) error {
	f, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer f.Close()
	return replaceExecutableFromReader(target, f)
}

func replaceExecutable(target string, content []byte) error {
	return replaceExecutableFromReader(target, bytes.NewReader(content))
}

// replaceExecutableFromReader writes the streamed binary beside the target and
// renames it into place. On Linux the running process keeps the old inode alive,
// so this is safe while the program is still executing; after exit the manager
// (systemd/procd) starts the fresh file.
func replaceExecutableFromReader(target string, src io.Reader) error {
	dir := filepath.Dir(target)
	tmp, err := os.CreateTemp(dir, ".viya-update-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := io.Copy(tmp, src); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0755); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), target)
}

// versionUpdateHandler downloads the latest release and swaps the binaries in
// place, then exits so the service manager restarts the process on the new
// build. Admin-only: a viewer write is rejected by the shared middleware.
func (a *App) versionUpdateHandler(w http.ResponseWriter, r *http.Request) {
	if !updateRunMu.TryLock() {
		fail(w, 409, errors.New("更新正在进行，请等待完成"))
		return
	}
	defer updateRunMu.Unlock()
	if !canSelfUpdate() {
		fail(w, 400, errors.New("当前运行环境不支持自动更新（容器或非 Linux），请拉取新镜像或手动替换二进制"))
		return
	}
	rel, err := fetchLatestRelease(8 * time.Second)
	if err != nil {
		fail(w, 502, err)
		return
	}
	if !versionNewer(version, rel.Tag) {
		fail(w, 400, errors.New("当前已是最新版本"))
		return
	}
	setVersionUpdateWarning("")
	setVersionUpdateProgress("准备更新", 1, nil)
	slog.Info("self-update started", "from", version, "to", rel.Tag)
	if err := a.downloadAndSwap(rel.Tag); err != nil {
		setVersionUpdateProgress("更新失败", 0, err)
		fail(w, 502, err)
		return
	}
	warning := currentUpdateWarning()
	// Exiting without a supervisor would leave the freshly installed binary
	// running nowhere. Keep the old process alive and ask the operator to
	// restart it instead.
	if !supervisedRestart() {
		slog.Warn("self-update installed; manual restart required", "version", rel.Tag)
		setVersionUpdateProgress("等待手动重启", 100, nil)
		writeJSON(w, 200, map[string]any{"ok": true, "restarting": false, "restart_required": true, "version": rel.Tag, "warning": warning})
		return
	}
	slog.Info("self-update applied, restarting", "version", rel.Tag)
	setVersionUpdateProgress("正在重启", 99, nil)
	writeJSON(w, 200, map[string]any{"ok": true, "restarting": true, "version": rel.Tag, "warning": warning})
	_ = requestProcessRestartWith(true, "self-update")
}
