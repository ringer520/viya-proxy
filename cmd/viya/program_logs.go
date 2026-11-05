package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"gopkg.in/natefinch/lumberjack.v2"
)

type LogSettings struct {
	Level     string `json:"level"`
	AccessMB  int    `json:"access_mb"`
	ProgramMB int    `json:"program_mb"`
	Backups   int    `json:"backups"`
	Days      int    `json:"days"`
}

func defaultLogSettings() LogSettings { return LogSettings{"INFO", 100, 20, 5, 30} }
func validateLogSettings(v *LogSettings) error {
	v.Level = strings.ToUpper(v.Level)
	switch v.Level {
	case "DEBUG", "INFO", "WARN", "ERROR":
	default:
		return errors.New("日志级别无效")
	}
	if v.AccessMB < 1 || v.AccessMB > 1024 || v.ProgramMB < 1 || v.ProgramMB > 1024 || v.Backups < 1 || v.Backups > 30 || v.Days < 1 || v.Days > 365 {
		return errors.New("日志大小 1–1024 MiB；备份文件 1–30 个；保留 1–365 天")
	}
	return nil
}
func (a *App) loadLogSettings() (LogSettings, error) {
	v := defaultLogSettings()
	var raw string
	e := a.db.QueryRow(`SELECT value FROM panel_settings WHERE key='log_settings'`).Scan(&raw)
	if errors.Is(e, sql.ErrNoRows) {
		return v, nil
	}
	if e != nil {
		return v, e
	}
	if json.Unmarshal([]byte(raw), &v) != nil {
		return v, errors.New("已保存的日志设置无效")
	}
	return v, validateLogSettings(&v)
}

var programLevel slog.LevelVar
var programOutput struct {
	sync.Mutex
	writer *lumberjack.Logger
}

type programWriter struct{}

func (programWriter) Write(p []byte) (int, error) {
	programOutput.Lock()
	defer programOutput.Unlock()
	if programOutput.writer == nil {
		return len(p), nil
	}
	return programOutput.writer.Write(p)
}
func (a *App) configureProgramLogging() {
	v, e := a.loadLogSettings()
	if e != nil {
		v = defaultLogSettings()
	}
	programOutput.Lock()
	if programOutput.writer != nil {
		_ = programOutput.writer.Close()
	}
	programOutput.writer = &lumberjack.Logger{Filename: filepath.Join(a.dataDir, "program.log"), MaxSize: v.ProgramMB, MaxBackups: v.Backups, MaxAge: v.Days, Compress: true}
	programOutput.Unlock()
	var level slog.Level
	_ = level.UnmarshalText([]byte(v.Level))
	programLevel.Set(level)
	slog.SetDefault(slog.New(slog.NewJSONHandler(io.MultiWriter(programWriter{}, os.Stderr), &slog.HandlerOptions{Level: &programLevel, ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
		if a.Value.Kind() == slog.KindString {
			return slog.String(a.Key, sanitizeProgramText(a.Value.String()))
		}
		if a.Value.Kind() == slog.KindAny {
			if e, ok := a.Value.Any().(error); ok {
				return slog.String(a.Key, sanitizeProgramText(e.Error()))
			}
		}
		return a
	}})))
}

var logURL = regexp.MustCompile(`https?://[^\s"<>]+`)

func sanitizeProgramText(s string) string {
	return logURL.ReplaceAllStringFunc(s, func(raw string) string {
		u, e := url.Parse(raw)
		if e != nil {
			return "[redacted URL]"
		}
		u.User = nil
		if u.RawQuery != "" {
			u.RawQuery = "redacted"
		}
		u.Fragment = ""
		return u.String()
	})
}
func (a *App) recentProgramLogs(w http.ResponseWriter, r *http.Request) {
	query, err := parseLogQuery(r)
	if err != nil {
		fail(w, 400, err)
		return
	}
	// Read only the structured, redacted program log; never shell out to journalctl.
	f, e := os.Open(filepath.Join(a.dataDir, "program.log"))
	if errors.Is(e, os.ErrNotExist) {
		writeJSON(w, 200, []any{})
		return
	}
	if e != nil {
		fail(w, 500, e)
		return
	}
	defer f.Close()
	stat, e := f.Stat()
	if e != nil {
		fail(w, 500, e)
		return
	}
	offset := stat.Size() - (2 << 20)
	if offset > 0 {
		_, _ = f.Seek(offset, 0)
	}
	raw, e := io.ReadAll(io.LimitReader(f, 2<<20))
	if e != nil {
		fail(w, 500, e)
		return
	}
	out := []map[string]any{}
	for _, line := range strings.Split(string(raw), "\n") {
		var v map[string]any
		if json.Unmarshal([]byte(line), &v) == nil && v != nil && query.matches(v) {
			out = append(out, v)
		}
	}
	if len(out) > query.Limit {
		out = out[len(out)-query.Limit:]
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	writeJSON(w, 200, out)
}
func (a *App) getLogSettings(w http.ResponseWriter, r *http.Request) {
	v, e := a.loadLogSettings()
	if e != nil {
		fail(w, 500, e)
		return
	}
	writeJSON(w, 200, v)
}
func (a *App) updateLogSettings(w http.ResponseWriter, r *http.Request) {
	var v LogSettings
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&v) != nil {
		fail(w, 400, errors.New("日志设置无效"))
		return
	}
	if e := validateLogSettings(&v); e != nil {
		fail(w, 400, e)
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	old, e := a.loadLogSettings()
	if e != nil {
		fail(w, 500, e)
		return
	}
	raw, _ := json.Marshal(v)
	if _, e = a.db.ExecContext(r.Context(), `INSERT INTO panel_settings(key,value) VALUES('log_settings',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, string(raw)); e != nil {
		fail(w, 500, e)
		return
	}
	if e = a.applyLocked(r.Context()); e != nil {
		raw, _ = json.Marshal(old)
		ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 20*time.Second)
		defer cancel()
		if _, err := a.db.ExecContext(ctx, `UPDATE panel_settings SET value=? WHERE key='log_settings'`, string(raw)); err != nil {
			fail(w, 500, errors.New("日志设置恢复失败，请检查配置历史"))
			return
		}
		a.confirmRestoredApplication(ctx, nil)
		fail(w, 502, errors.New("日志设置未能应用，已恢复原设置"))
		return
	}
	a.configureProgramLogging()
	slog.Info("Log settings updated", "level", v.Level)
	writeJSON(w, 200, v)
}
