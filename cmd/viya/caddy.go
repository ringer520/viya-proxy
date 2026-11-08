package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func (a *App) startCaddy(dataDir string) error {
	bin := a.caddyBin
	if _, err := exec.LookPath(bin); err != nil {
		return fmt.Errorf("Caddy binary %q is unavailable", bin)
	}
	initialConfig, _ := json.Marshal(map[string]any{"admin": map[string]any{"listen": a.adminListen, "config": map[string]any{"persist": false}}, "apps": map[string]any{}})
	initial := initialConfig
	configPath := filepath.Join(dataDir, "bootstrap-caddy.json")
	if err := os.WriteFile(configPath, initial, 0600); err != nil {
		return err
	}
	ready := make(chan error, 1)
	go a.superviseCaddy(configPath, ready)
	select {
	case err := <-ready:
		return err
	case <-time.After(12 * time.Second):
		return errors.New("等待 Caddy Admin API 超时")
	case <-a.ctx.Done():
		return errors.New("启动被中断")
	}
}

func (a *App) superviseCaddy(configPath string, firstReady chan<- error) {
	first := true
	backoff := time.Second
	for a.ctx.Err() == nil {
		cmd := exec.Command(a.caddyBin, "run", "--config", configPath)
		stdoutLog := &caddyLogWriter{app: a, stream: "stdout"}
		stderrLog := &caddyLogWriter{app: a, stream: "stderr"}
		cmd.Stdout, cmd.Stderr = stdoutLog, stderrLog
		if err := cmd.Start(); err != nil {
			if first {
				firstReady <- err
				first = false
			}
			slog.Error("start Caddy", "error", err, "retry_in", backoff)
			if !waitContext(a.ctx, backoff) {
				return
			}
			backoff = min(backoff*2, 30*time.Second)
			continue
		}
		a.caddyPID.Store(int64(cmd.Process.Pid))
		slog.Info("Caddy process started", "pid", cmd.Process.Pid)
		done := make(chan struct{})
		var processErr error
		go func() {
			processErr = cmd.Wait()
			a.caddyPID.CompareAndSwap(int64(cmd.Process.Pid), 0)
			stdoutLog.Flush()
			stderrLog.Flush()
			close(done)
		}()
		readyErr := waitAdmin(a.ctx, done, &processErr, a.adminURL, 10*time.Second)
		if readyErr == nil {
			a.setCaddyReady(true)
			if err := a.apply(a.ctx); err != nil {
				slog.Error("saved proxy rules could not be applied; inspect the program logs")
			} else {
				slog.Info("saved proxy rules reconciled")
			}
			if first {
				firstReady <- nil
				first = false
			}
			backoff = time.Second
		} else {
			a.setCaddyReady(false)
			if first {
				firstReady <- readyErr
				first = false
			}
			_ = cmd.Process.Kill()
		}
		select {
		case <-done:
			if a.ctx.Err() != nil {
				return
			}
			a.setCaddyReady(false)
			slog.Error("Caddy exited; restarting", "error", processErr, "retry_in", backoff)
			a.notify("caddy_exited", "Caddy 进程退出",
				fmt.Sprintf("代理引擎已退出，将在 %.0f 秒后重启。错误：%v", backoff.Seconds(), processErr))
		case <-a.ctx.Done():
			_ = cmd.Process.Signal(os.Interrupt)
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				_ = cmd.Process.Kill()
				<-done
			}
			return
		}
		if !waitContext(a.ctx, backoff) {
			return
		}
		backoff = min(backoff*2, 30*time.Second)
	}
}

func waitAdmin(ctx context.Context, done <-chan struct{}, processErr *error, adminURL string, timeout time.Duration) error {
	client := http.Client{Timeout: time.Second}
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		resp, err := client.Get(strings.TrimRight(adminURL, "/") + "/config/")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode < 500 {
				return nil
			}
		}
		select {
		case <-done:
			return fmt.Errorf("Caddy exited before Admin API became ready: %w", *processErr)
		case <-ticker.C:
		case <-deadline.C:
			return errors.New("等待 Caddy Admin API 超时")
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func waitContext(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}

func (a *App) setCaddyReady(ready bool) {
	a.caddyReady.Store(ready)
}

func (a *App) isCaddyReady() bool {
	return a.caddyReady.Load()
}

func caddyAdminListen(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" || u.User != nil || u.Path != "" && u.Path != "/" || u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("Caddy Admin API 必须是带主机和端口的本地 HTTP 地址")
	}
	host, port, err := net.SplitHostPort(u.Host)
	if err != nil || port == "" {
		return "", errors.New("Caddy Admin API 必须包含端口")
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		return "", errors.New("Caddy Admin API 端口无效")
	}
	ip := net.ParseIP(host)
	if !(ip != nil && ip.IsLoopback() || strings.EqualFold(host, "localhost")) {
		return "", errors.New("Caddy Admin API 必须使用回环地址")
	}
	return net.JoinHostPort(host, port), nil
}

func validateListenAddress(address string) error {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return errors.New("监听地址必须是 IP 加端口，例如 :8080")
	}
	if host != "" {
		ip := net.ParseIP(host)
		if ip == nil {
			return errors.New("监听主机必须是 IP 地址或留空")
		}
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		return errors.New("监听端口无效")
	}
	return nil
}
