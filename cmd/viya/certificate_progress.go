package main

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"time"
)

type certificateProgress struct {
	Status    string
	Percent   int
	Message   string
	LastError string
	Attempt   int
	UpdatedAt time.Time
}

func (a *App) setCertificateProgress(domain string, progress certificateProgress) {
	domain = strings.ToLower(strings.TrimSpace(domain))
	if domain == "" {
		return
	}
	progress.UpdatedAt = time.Now().UTC()
	a.certProgressMu.Lock()
	if a.certProgress == nil {
		a.certProgress = make(map[string]certificateProgress)
	}
	a.certProgress[domain] = progress
	a.certProgressMu.Unlock()
}

func (a *App) getCertificateProgress(domain string) certificateProgress {
	a.certProgressMu.RLock()
	progress, ok := a.certProgress[strings.ToLower(strings.TrimSpace(domain))]
	a.certProgressMu.RUnlock()
	if !ok {
		return certificateProgress{Status: "pending", Percent: 8, Message: "申请已保存，等待 Caddy 开始签发"}
	}
	return progress
}

type caddyLogWriter struct {
	app    *App
	stream string
	mu     sync.Mutex
	buffer []byte
}

func (w *caddyLogWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buffer = append(w.buffer, p...)
	for {
		index := bytes.IndexByte(w.buffer, '\n')
		if index < 0 {
			break
		}
		w.writeLine(strings.TrimSuffix(string(w.buffer[:index]), "\r"))
		w.buffer = w.buffer[index+1:]
	}
	if len(w.buffer) > 1<<20 {
		w.writeLine(string(w.buffer))
		w.buffer = nil
	}
	return len(p), nil
}

func (w *caddyLogWriter) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.buffer) > 0 {
		w.writeLine(string(w.buffer))
		w.buffer = nil
	}
}

func (w *caddyLogWriter) writeLine(line string) {
	w.app.observeCertificateLog(line)
	var event map[string]any
	if json.Unmarshal([]byte(line), &event) != nil {
		slog.Info("Caddy output", "source", "caddy")
		return
	}
	level := slog.LevelInfo
	switch event["level"] {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error", "fatal", "panic":
		level = slog.LevelError
	}
	attrs := []slog.Attr{slog.String("source", "caddy")}
	for _, key := range []string{"logger", "error", "identifier", "domain", "challenge_type"} {
		if value, ok := event[key].(string); ok {
			attrs = append(attrs, slog.String(key, sanitizeProgramText(value)))
		}
	}
	message, _ := event["msg"].(string)
	slog.LogAttrs(context.Background(), level, sanitizeProgramText(message), attrs...)
}

func (a *App) observeCertificateLog(line string) {
	var fields map[string]any
	_ = json.Unmarshal([]byte(line), &fields)
	textField := func(name string) string {
		if value, ok := fields[name].(string); ok {
			return value
		}
		return ""
	}
	message := strings.ToLower(textField("msg") + " " + textField("error"))
	lowerLine := strings.ToLower(line)
	challenge := strings.ToLower(textField("challenge_type"))
	identifier := strings.ToLower(textField("identifier"))
	if identifier == "" {
		identifier = strings.ToLower(textField("domain"))
	}
	if message == "  " || message == "" {
		return
	}

	a.certProgressMu.RLock()
	domains := make([]string, 0, len(a.certProgress))
	for domain := range a.certProgress {
		domains = append(domains, domain)
	}
	a.certProgressMu.RUnlock()
	for _, domain := range domains {
		base := strings.TrimPrefix(domain, "*.")
		matches := strings.Contains(identifier, base) || strings.Contains(lowerLine, domain) || (strings.HasPrefix(domain, "*.") && strings.Contains(identifier, base))
		if !matches {
			continue
		}
		previous := a.getCertificateProgress(domain)
		next := previous
		switch {
		case strings.Contains(message, "challenge failed") || strings.Contains(message, "authorization failed") || strings.Contains(message, "certificate issuance failed") || strings.Contains(message, "error"):
			next.Status, next.Percent = "retrying", 48
			next.Message = "验证遇到问题，Caddy 将自动重试"
			next.LastError = "CA 验证或签发暂未成功；可查看服务日志，Caddy 会自动重试。"
			next.Attempt++
		case strings.Contains(message, "certificate obtained successfully") || strings.Contains(message, "obtained certificate") || strings.Contains(message, "certificate issued"):
			next.Status, next.Percent = "issued", 100
			next.Message, next.LastError = "证书已签发，正在刷新有效期", ""
		case strings.Contains(message, "trying to solve challenge") || strings.Contains(message, "solving challenge") || strings.Contains(message, "presenting dns") || strings.Contains(message, "propagation"):
			next.Status, next.Percent = "validating", 45
			next.Message = "正在验证域名所有权"
			if challenge != "" {
				next.Message += "（" + strings.ToUpper(challenge) + "）"
			}
			next.LastError = ""
		case strings.Contains(message, "obtaining certificate") || strings.Contains(message, "attempting to obtain") || strings.Contains(message, "waiting on internal rate limiter"):
			next.Status, next.Percent = "obtaining", 22
			next.Message, next.LastError = "Caddy 正在连接证书颁发机构", ""
		default:
			continue
		}
		a.setCertificateProgress(domain, next)
	}
}
