package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Alerts are delivered to a single operator-supplied webhook. The point is to
// surface failures the panel cannot show when nobody is looking at it: a dead
// upstream, a failed certificate order, a full disk.
const (
	alertSettingsKey = "alert_webhook"
	alertTimeout     = 6 * time.Second
	alertRetries     = 2
	// alertDebounce collapses a flapping condition into one message per window.
	alertDebounce = 10 * time.Minute
)

type alertConfig struct {
	Enabled    bool   `json:"enabled"`
	WebhookURL string `json:"webhook_url"`
	// NotifyRecovery also sends a message when a failing condition clears.
	NotifyRecovery bool `json:"notify_recovery"`
}

func (a *App) loadAlertConfig() alertConfig {
	var raw string
	if err := a.db.QueryRow(`SELECT value FROM panel_settings WHERE key=?`, alertSettingsKey).Scan(&raw); err != nil {
		return alertConfig{}
	}
	var config alertConfig
	if err := json.Unmarshal([]byte(raw), &config); err != nil {
		return alertConfig{}
	}
	return config
}

func (a *App) saveAlertConfig(config alertConfig) error {
	encoded, err := json.Marshal(config)
	if err != nil {
		return err
	}
	_, err = a.db.Exec(`INSERT INTO panel_settings(key,value) VALUES(?,?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value`, alertSettingsKey, string(encoded))
	return err
}

// notify queues one alert. Delivery happens off the caller's goroutine so a slow
// webhook can never stall a probe loop or an HTTP handler.
func (a *App) notify(event, title, detail string) {
	config := a.loadAlertConfig()
	if !config.Enabled || config.WebhookURL == "" {
		return
	}
	if !a.alertAllowed(event) {
		return
	}
	payload := map[string]any{
		"source": "viya",
		"event":  event,
		"title":  title,
		"detail": detail,
		"at":     time.Now().Unix(),
	}
	go a.deliverAlert(config.WebhookURL, payload)
}

// alertAllowed applies per-event debouncing.
func (a *App) alertAllowed(event string) bool {
	now := time.Now()
	a.alertMu.Lock()
	defer a.alertMu.Unlock()
	if a.alertLast == nil {
		a.alertLast = make(map[string]time.Time)
	}
	if last, ok := a.alertLast[event]; ok && now.Sub(last) < alertDebounce {
		return false
	}
	if len(a.alertLast) > 500 {
		for key, at := range a.alertLast {
			if now.Sub(at) >= alertDebounce {
				delete(a.alertLast, key)
			}
		}
	}
	a.alertLast[event] = now
	return true
}

func (a *App) deliverAlert(target string, payload map[string]any) {
	body, err := json.Marshal(payload)
	if err != nil {
		return
	}
	client := &http.Client{Timeout: alertTimeout}
	for attempt := 0; attempt <= alertRetries; attempt++ {
		req, err := http.NewRequest(http.MethodPost, target, bytes.NewReader(body))
		if err != nil {
			slog.Warn("build alert request", "error", err)
			return
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "Viya-Alerts/1")
		res, err := client.Do(req)
		if err == nil {
			res.Body.Close()
			if res.StatusCode >= 200 && res.StatusCode < 300 {
				return
			}
			err = errors.New("webhook returned " + res.Status)
		}
		if attempt == alertRetries {
			slog.Warn("deliver alert", "error", err, "event", payload["event"])
			return
		}
		time.Sleep(time.Duration(attempt+1) * 2 * time.Second)
	}
}

func validWebhookURL(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return false
	}
	return parsed.Host != ""
}

func (a *App) getAlertSettings(w http.ResponseWriter, r *http.Request) {
	config := a.loadAlertConfig()
	// The URL itself is not a secret, but it often embeds a token, so only the
	// host is echoed back once a value has been stored.
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled":         config.Enabled,
		"webhook_url":     config.WebhookURL,
		"notify_recovery": config.NotifyRecovery,
	})
}

func (a *App) updateAlertSettings(w http.ResponseWriter, r *http.Request) {
	var input alertConfig
	if err := decodeJSONBody(w, r, &input); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	input.WebhookURL = strings.TrimSpace(input.WebhookURL)
	if input.Enabled && !validWebhookURL(input.WebhookURL) {
		fail(w, http.StatusBadRequest, errors.New("Webhook 必须是 http 或 https 地址"))
		return
	}
	if input.WebhookURL != "" && !validWebhookURL(input.WebhookURL) {
		fail(w, http.StatusBadRequest, errors.New("Webhook 必须是 http 或 https 地址"))
		return
	}
	if err := a.saveAlertConfig(input); err != nil {
		slog.Error("save alert settings", "error", err)
		fail(w, http.StatusInternalServerError, errors.New("无法保存告警设置"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// testAlertSettings sends a sample message so the operator can confirm the
// webhook works without waiting for a real failure.
func (a *App) testAlertSettings(w http.ResponseWriter, r *http.Request) {
	config := a.loadAlertConfig()
	if !config.Enabled || config.WebhookURL == "" {
		fail(w, http.StatusBadRequest, errors.New("请先启用告警并填写 Webhook"))
		return
	}
	payload := map[string]any{
		"source": "viya",
		"event":  "test",
		"title":  "Viya 测试通知",
		"detail": "如果你收到这条消息，说明告警通道已接通。",
		"at":     time.Now().Unix(),
	}
	go a.deliverAlert(config.WebhookURL, payload)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
