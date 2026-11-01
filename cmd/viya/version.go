package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
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
	"strconv"
	"strings"
	"sync"
	"time"
)

// version is stamped at build time:
//
//	go build -ldflags "-X main.version=v1.2.3"
//
// Unreleased builds keep the "dev" marker so the update button stays hidden.
var version = "dev"

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
	updateCacheRelease *releaseInfo
	updateCacheAt      time.Time
	updateProgressMu   sync.RWMutex
	updateProgress     = versionUpdateProgress{Stage: "idle", Percent: 0}
)

type versionUpdateProgress struct {
	Stage   string `json:"stage"`
	Percent int    `json:"percent"`
	Error   string `json:"error,omitempty"`
}

func setVersionUpdateProgress(stage string, percent int, err error) {
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}
	updateProgressMu.Lock()
	updateProgress = versionUpdateProgress{Stage: stage, Percent: percent}
	if err != nil {
		updateProgress.Error = err.Error()
	}
	updateProgressMu.Unlock()
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
		Current:  version,
		InDocker: isDockerized(),
		Managed:  canSelfUpdate(),
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

// downloadAndSwap fetches the release archive for this machine's architecture,
// verifies it against the published sha256sums, and atomically replaces the
// running Viya binary plus its bundled Caddy. The caller must have already
// decided the update is allowed.
func (a *App) downloadAndSwap(tag string) error {
	setVersionUpdateProgress("准备校验", 2, nil)
	arch := runtime.GOARCH
	if arch == "arm" {
		arch = "armv7"
	}
	base := fmt.Sprintf("https://github.com/%s/%s/releases/download/%s", updateRepoOwner, updateRepoName, tag)
	asset := fmt.Sprintf("viya-linux-%s.tar.gz", arch)

	sums, err := httpGetLimited(base+"/sha256sums.txt", 1<<20)
	if err != nil {
		return errors.New("无法下载校验文件")
	}
	wantSum := ""
	for _, line := range strings.Split(string(sums), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && strings.HasSuffix(fields[1], asset) {
			wantSum = fields[0]
			break
		}
	}
	if wantSum == "" {
		return errors.New("校验文件中找不到当前架构的产物")
	}

	setVersionUpdateProgress("正在下载更新包", 4, nil)
	archiveBytes, err := httpGetWithProgress(base+"/"+asset, 256<<20, func(downloaded, total int64) {
		percent := 4
		if total > 0 {
			percent += int(float64(downloaded) / float64(total) * 82)
		}
		setVersionUpdateProgress("正在下载更新包", percent, nil)
	})
	if err != nil {
		return errors.New("无法下载新版本，请检查服务器网络")
	}
	sum := sha256.Sum256(archiveBytes)
	if hex.EncodeToString(sum[:]) != wantSum {
		return errors.New("下载文件校验失败，已取消更新")
	}
	setVersionUpdateProgress("校验更新包", 88, nil)

	gz, err := gzip.NewReader(bytes.NewReader(archiveBytes))
	if err != nil {
		return errors.New("无法解析新版本压缩包")
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	var viyaBin, caddyBin []byte
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return errors.New("无法读取新版本压缩包")
		}
		if hdr.Typeflag != tar.TypeReg || hdr.Size > 256<<20 {
			continue
		}
		name := filepath.Base(hdr.Name)
		if strings.HasPrefix(name, "viya-linux-") {
			viyaBin, err = io.ReadAll(tr)
		} else if strings.HasPrefix(name, "caddy-linux-") {
			caddyBin, err = io.ReadAll(tr)
		}
		if err != nil {
			return errors.New("无法读取新版本二进制")
		}
	}
	if viyaBin == nil || caddyBin == nil {
		return errors.New("新版本压缩包缺少二进制文件")
	}
	setVersionUpdateProgress("替换程序文件", 94, nil)

	selfPath, err := os.Executable()
	if err != nil {
		return errors.New("无法定位当前程序路径")
	}
	if err := replaceExecutable(selfPath, viyaBin); err != nil {
		return fmt.Errorf("无法替换主程序：%w", err)
	}
	caddyPath := a.caddyBin
	if caddyPath == "" {
		caddyPath = filepath.Join(filepath.Dir(selfPath), "caddy")
	}
	// Caddy is a subprocess, so a missing path is not fatal: the new binary
	// can still come up and the user can fix the engine path later.
	_ = replaceExecutable(caddyPath, caddyBin)
	setVersionUpdateProgress("正在重启", 97, nil)
	return nil
}

func httpGetWithProgress(url string, limit int64, progress func(downloaded, total int64)) ([]byte, error) {
	client := &http.Client{Timeout: 5 * time.Minute}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", updateRepoName)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	var buf bytes.Buffer
	reader := io.LimitReader(resp.Body, limit+1)
	chunk := make([]byte, 64*1024)
	var downloaded int64
	for {
		n, readErr := reader.Read(chunk)
		if n > 0 {
			downloaded += int64(n)
			if downloaded > limit {
				return nil, errors.New("download exceeds size limit")
			}
			if _, err := buf.Write(chunk[:n]); err != nil {
				return nil, err
			}
			if progress != nil {
				progress(downloaded, resp.ContentLength)
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return nil, readErr
		}
	}
	return buf.Bytes(), nil
}

// replaceExecutable writes the new content beside the target and renames it
// into place. On Linux the running process keeps the old inode alive, so this
// is safe while the program is still executing; after exit the manager
// (systemd/procd) starts the fresh file.
func replaceExecutable(target string, content []byte) error {
	dir := filepath.Dir(target)
	tmp, err := os.CreateTemp(dir, ".viya-update-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(content); err != nil {
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

func httpGetLimited(url string, limit int64) ([]byte, error) {
	client := &http.Client{Timeout: 5 * time.Minute}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", updateRepoName)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, limit))
}

// versionUpdateHandler downloads the latest release and swaps the binaries in
// place, then exits so the service manager restarts the process on the new
// build. Admin-only: a viewer write is rejected by the shared middleware.
func (a *App) versionUpdateHandler(w http.ResponseWriter, r *http.Request) {
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
	setVersionUpdateProgress("准备更新", 1, nil)
	slog.Info("self-update started", "from", version, "to", rel.Tag)
	if err := a.downloadAndSwap(rel.Tag); err != nil {
		setVersionUpdateProgress("更新失败", 0, err)
		fail(w, 502, err)
		return
	}
	slog.Info("self-update applied, restarting", "version", rel.Tag)
	setVersionUpdateProgress("正在重启", 99, nil)
	writeJSON(w, 200, map[string]any{"ok": true, "restarting": true, "version": rel.Tag})
	// Give the response a moment to flush, then hand over to the service
	// manager (systemd Restart=always / procd respawn) which starts the new
	// binary.
	go func() {
		time.Sleep(700 * time.Millisecond)
		os.Exit(0)
	}()
}
