package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

func (a *App) applyLocked(ctx context.Context) (result error) {
	a.setApplyStage("compiling", "正在生成配置")
	defer func() {
		if result != nil {
			a.setApplyStage("failed", "应用失败，当前引擎继续使用原配置；请查看配置历史")
		}
	}()
	body, err := a.compileConfig(ctx)
	if err != nil {
		return err
	}
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
		a.recordRevision(ctx, body, "failed", cause)
		return cause
	}
	a.setApplyStage("applying", "正在平滑加载配置")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(a.adminURL, "/")+"/load", strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	client := http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		a.recordRevision(ctx, body, "failed", err)
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		cause := fmt.Errorf("Caddy returned %s", resp.Status)
		a.recordRevision(ctx, body, "failed", cause)
		return cause
	}
	a.recordRevision(ctx, body, "applied", nil)
	if err := a.saveCheckpoint(ctx, body); err != nil {
		a.setApplyStage("applied-warning", "配置已生效，但恢复点保存失败")
		return nil
	}
	a.setApplyStage("applied", "配置已生效")
	return nil
}
