package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

func (a *App) applyLocked(ctx context.Context) (result error) {
	// Once a mutation is saved, finish or recover independently of the browser.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 90*time.Second)
	defer cancel()
	a.setApplyStage("compiling", "正在生成配置")
	defer func() {
		if result != nil {
			a.setApplyStage("failed", "存在未生效的配置，请重试应用或使用恢复点恢复")
			if body, err := a.compileConfig(ctx); err == nil {
				a.journalApplication(body, false)
			}
		}
	}()
	body, err := a.compileConfig(ctx)
	if err != nil {
		return err
	}
	a.journalApplication(body, false)
	a.setApplyStage("validating", "正在校验 Caddy 配置")
	configFile, err := os.CreateTemp("", "viya-caddy-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(configFile.Name())
	if err := configFile.Chmod(0600); err != nil {
		configFile.Close()
		return err
	}
	if _, err := configFile.Write(body); err != nil {
		configFile.Close()
		return err
	}
	if err := configFile.Close(); err != nil {
		return err
	}
	validate := exec.CommandContext(ctx, a.caddyBin, "validate", "--config", configFile.Name())
	if output, err := validate.CombinedOutput(); err != nil {
		cause := fmt.Errorf("caddy validate: %s: %w", strings.TrimSpace(string(output)), err)
		return cause
	}
	a.setApplyStage("applying", "正在平滑加载配置")
	previous, err := a.livePolicyConfig(ctx)
	if err != nil {
		return fmt.Errorf("cannot snapshot running Caddy configuration; apply cancelled: %w", err)
	}
	loaded := false
	defer func() {
		if result != nil && !loaded {
			recoveryCtx, recoveryCancel := context.WithTimeout(context.Background(), 25*time.Second)
			defer recoveryCancel()
			if err := a.restoreLivePolicyConfig(recoveryCtx, previous); err != nil {
				result = fmt.Errorf("application failed and runtime recovery could not be confirmed: %w", err)
			}
		}
	}()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(a.adminURL, "/")+"/load", strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	client := http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		// Drain bounded error output for connection reuse, without exposing secrets.
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		cause := fmt.Errorf("Caddy returned %s", resp.Status)
		return cause
	}
	loaded = true
	a.journalApplication(body, true)
	if err := a.cleanupDeletedSites(ctx); err != nil {
		slog.Warn("deferred static file cleanup failed; will retry after next apply", "error", err)
	}
	if err := a.saveCheckpoint(ctx, body); err != nil {
		a.setApplyStage("applied-warning", "配置已生效，但恢复点保存失败")
		a.journalApplication(body, true)
		return nil
	}
	a.setApplyStage("applied", "配置已生效")
	a.journalApplication(body, true)
	return nil
}
