package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"
)

func (a *App) apply(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.applyLocked(ctx)
}

// scheduleApply coalesces bursty configuration changes, notably automatic IP
// bans, into at most one Caddy validate + load per window. Without it every
// banned request could fork a Caddy subprocess.

// scheduleApply coalesces bursty configuration changes, notably automatic IP
// bans, into at most one Caddy validate + load per window. Without it every
// banned request could fork a Caddy subprocess.
func (a *App) scheduleApply() {
	select {
	case a.applyKick <- struct{}{}:
	default:
	}
}

func (a *App) applyWorker() {
	for {
		select {
		case <-a.ctx.Done():
			return
		case <-a.applyKick:
			timer := time.NewTimer(500 * time.Millisecond)
			select {
			case <-a.ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
			// Drop pending kicks; the apply below already covers them.
			select {
			case <-a.applyKick:
			default:
			}
			ctx, cancel := context.WithTimeout(a.ctx, 20*time.Second)
			if err := a.apply(ctx); err != nil && a.ctx.Err() == nil {
				// Bans stay in the database; the next change or restart retries.
				slog.Warn("deferred Caddy apply failed", "error", err)
			}
			cancel()
		}
	}
}

// applyLocked requires a.mu; listener changes hold it across save, reload and recovery.

func (a *App) recordRevision(ctx context.Context, config []byte, status string, cause error) {
	errText := ""
	if cause != nil {
		errText = cause.Error()
	}
	operation, _ := ctx.Value(operationContextKey{}).(operationContext)
	if operation.Summary == "" {
		operation.Summary = "后台配置同步"
	}
	redacted, secrets := redactConfig(config)
	for _, secret := range secrets {
		errText = strings.ReplaceAll(errText, secret, "[redacted]")
	}
	if _, err := a.db.ExecContext(ctx, `INSERT INTO config_revisions(status,config_json,error_text,summary,actor) VALUES(?,?,?,?,?)`, status, string(redacted), errText, operation.Summary, operation.Actor); err != nil {
		slog.Error("record config revision", "error", err)
		return
	}
	// The UI only ever shows the newest 10 entries; keep a bounded history so
	// repeated applies cannot grow the database without limit.
	if _, err := a.db.ExecContext(ctx, `DELETE FROM config_revisions WHERE id NOT IN (SELECT id FROM config_revisions ORDER BY id DESC LIMIT 50)`); err != nil {
		slog.Warn("prune config revisions", "error", err)
	}
}

func redactConfig(config []byte) ([]byte, []string) {
	var value any
	if json.Unmarshal(config, &value) != nil {
		return []byte(`{"redacted":true}`), nil
	}
	secrets := []string{}
	var visit func(any, string)
	visit = func(current any, key string) {
		switch item := current.(type) {
		case map[string]any:
			for childKey, child := range item {
				if isSecretField(childKey) {
					if text, ok := child.(string); ok && text != "" {
						secrets = append(secrets, text)
					}
					item[childKey] = "[redacted]"
				} else {
					visit(child, childKey)
				}
			}
		case []any:
			for _, child := range item {
				visit(child, key)
			}
		}
	}
	visit(value, "")
	redacted, err := json.Marshal(value)
	if err != nil {
		return []byte(`{"redacted":true}`), secrets
	}
	return redacted, secrets
}

func isSecretField(key string) bool {
	lower := strings.ToLower(key)
	return strings.Contains(lower, "secret") || strings.Contains(lower, "token") || strings.Contains(lower, "password") || strings.Contains(lower, "access_key")
}
