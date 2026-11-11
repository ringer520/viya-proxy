package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"runtime/debug"
	"strings"

	"modernc.org/sqlite"
)

// statusRecorder tracks whether a response has started so recoverPanic can
// decide between a clean 500 and leaving an already-started response alone.
type statusRecorder struct {
	http.ResponseWriter
	wrote  bool
	status int
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func ensureColumn(db *sql.DB, table, column, definition string) error {
	rows, err := db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, dataType string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &primaryKey); err != nil {
			return err
		}
		if name == column {
			return rows.Err()
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	// Close the cursor before altering: SQLite refuses to write while a read
	// cursor on the same schema is still open on this connection.
	rows.Close()
	_, err = db.Exec(`ALTER TABLE ` + table + ` ADD COLUMN ` + column + ` ` + definition)
	return err
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// classifyHostWriteError turns SQLite constraint failures into actionable
// messages instead of reporting every failure as a duplicate domain.

// classifyHostWriteError turns SQLite constraint failures into actionable
// messages instead of reporting every failure as a duplicate domain.
func classifyHostWriteError(err error) (int, error) {
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "unique constraint failed: proxy_hosts.domain"):
		return http.StatusConflict, errors.New("该域名已被其他站点占用，请修改域名或改用其他端口监听")
	case strings.Contains(message, "foreign key constraint failed"):
		return http.StatusBadRequest, errors.New("所选的端口监听、DNS 凭据或自定义证书已不存在，请重新选择")
	case strings.Contains(message, "not null constraint failed"):
		return http.StatusBadRequest, errors.New("站点信息不完整，请检查必填字段")
	case strings.Contains(message, "check constraint failed"):
		return http.StatusBadRequest, errors.New("站点字段取值不被允许，请检查 TLS 模式与限流设置")
	}
	slog.Error("proxy host write failed", "error", err)
	return http.StatusInternalServerError, errors.New("站点保存失败，请稍后重试；详细原因已写入服务日志")
}

func fail(w http.ResponseWriter, status int, err error) {
	if status >= 500 {
		slog.Error("request failed", "status", status, "error", err)
	}
	// Engine, database and filesystem errors carry table names, columns,
	// absolute paths or Caddy config fragments; never echo them verbatim.
	// 502/504 messages from upstream calls stay readable: they are actionable.
	if status == http.StatusInternalServerError || status == http.StatusServiceUnavailable {
		if internalFailure(err) {
			writeJSON(w, status, map[string]string{"error": "服务器内部错误，请稍后重试；详细原因已写入服务日志"})
			return
		}
	}
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

func internalFailure(err error) bool {
	var sqliteErr *sqlite.Error
	if errors.As(err, &sqliteErr) {
		return true
	}
	var pathErr *os.PathError
	if errors.As(err, &pathErr) {
		return true
	}
	var linkErr *os.LinkError
	if errors.As(err, &linkErr) {
		return true
	}
	var syscallErr *os.SyscallError
	if errors.As(err, &syscallErr) {
		return true
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return true
	}
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return true
	}
	message := err.Error()
	return strings.HasPrefix(message, "sql: ") || strings.HasPrefix(message, "caddy validate:") || strings.Contains(message, "modernc.org/sqlite")
}

// recoverPanic keeps a panic in any handler from tearing down the connection
// (which the browser surfaces as a bare "Failed to fetch"). It logs the stack
// and, when nothing has been written yet, returns a clean 500 JSON so the UI
// can show the real failure instead of a network error.

// recoverPanic keeps a panic in any handler from tearing down the connection
// (which the browser surfaces as a bare "Failed to fetch"). It logs the stack
// and, when nothing has been written yet, returns a clean 500 JSON so the UI
// can show the real failure instead of a network error.
func recoverPanic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := &statusRecorder{ResponseWriter: w}
		defer func() {
			if p := recover(); p != nil {
				slog.Error("panic recovered in handler",
					"method", r.Method,
					"path", r.URL.Path,
					"panic", fmt.Sprint(p),
					"stack", string(debug.Stack()),
				)
				if !rec.wrote {
					rec.Header().Set("Content-Type", "application/json")
					rec.WriteHeader(http.StatusInternalServerError)
					_, _ = rec.Write([]byte(`{"error":"服务器内部错误，请查看运行日志"}`))
				}
			}
		}()
		next.ServeHTTP(rec, r)
	})
}

// statusRecorder tracks whether a response has started so recoverPanic can
// decide between a clean 500 and leaving an already-started response alone.

func (r *statusRecorder) WriteHeader(code int) {
	if !r.wrote {
		r.wrote = true
		r.status = code
	}
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if !r.wrote {
		r.wrote = true
		r.status = http.StatusOK
	}
	return r.ResponseWriter.Write(b)
}
