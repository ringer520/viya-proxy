package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"viya/internal/secretstore"

	"golang.org/x/crypto/scrypt"
)

const configFormat = "viya-config-v3"

var configTables = []string{"dns_providers", "custom_certificates", "managed_certificates", "proxy_hosts", "layer4_rules", "ddns_records", "ip_bans", "panel_settings", "monitors", "config_revisions"}
var runtimeTables = []string{"sessions", "traffic_hours", "site_access_events", "ddns_runs", "monitor_samples"}

type configCell struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}
type tableDump struct {
	Name    string         `json:"name"`
	Columns []string       `json:"columns"`
	Rows    [][]configCell `json:"rows"`
}
type configFile struct {
	Path string `json:"path"`
	Data []byte `json:"data"`
}
type configPayload struct {
	Format      string            `json:"format"`
	Created     string            `json:"created"`
	SourceDir   string            `json:"source_dir"`
	Key         []byte            `json:"key"`
	Tables      []tableDump       `json:"tables"`
	Files       []configFile      `json:"files"`
	Preferences map[string]string `json:"preferences"`
}

// configEnvelope is the archive container. Archives produced by this build are
// always encrypted with a key derived from the operator's backup password;
// Payload survives only so password-free archives from older builds remain
// importable.
type configEnvelope struct {
	Format     string         `json:"format"`
	Created    string         `json:"created,omitempty"`
	Encrypted  bool           `json:"encrypted,omitempty"`
	KDF        string         `json:"kdf,omitempty"`
	Salt       []byte         `json:"salt,omitempty"`
	Nonce      []byte         `json:"nonce,omitempty"`
	Ciphertext []byte         `json:"ciphertext,omitempty"`
	Payload    *configPayload `json:"payload,omitempty"`
}
type configImportInput struct {
	Archive   configEnvelope `json:"archive"`
	Password  string         `json:"password"`
	Confirmed bool           `json:"confirmed"`
}

const (
	archiveAAD         = "viya:config-archive:v1"
	archiveScryptN     = 1 << 15
	archiveScryptR     = 8
	archiveScryptP     = 1
	archiveMinPassword = 8
)

type sqlReader interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func quoteColumn(s string) string { return `"` + strings.ReplaceAll(s, `"`, `""`) + `"` }
func captureTables(ctx context.Context, db sqlReader, names []string) ([]tableDump, error) {
	out := []tableDump{}
	for _, name := range names {
		rows, err := db.QueryContext(ctx, `SELECT * FROM `+quoteColumn(name))
		if err != nil {
			return nil, err
		}
		columns, err := rows.Columns()
		if err != nil {
			rows.Close()
			return nil, err
		}
		table := tableDump{Name: name, Columns: columns, Rows: [][]configCell{}}
		for rows.Next() {
			values := make([]any, len(columns))
			ptrs := make([]any, len(columns))
			for i := range values {
				ptrs[i] = &values[i]
			}
			if err = rows.Scan(ptrs...); err != nil {
				rows.Close()
				return nil, err
			}
			cells := make([]configCell, len(values))
			for i, v := range values {
				switch x := v.(type) {
				case nil:
					cells[i] = configCell{Type: "null"}
				case []byte:
					cells[i] = configCell{Type: "blob", Value: base64.StdEncoding.EncodeToString(x)}
				case string:
					cells[i] = configCell{Type: "text", Value: x}
				case int64:
					cells[i] = configCell{Type: "integer", Value: strconv.FormatInt(x, 10)}
				case float64:
					cells[i] = configCell{Type: "real", Value: strconv.FormatFloat(x, 'g', -1, 64)}
				default:
					rows.Close()
					return nil, fmt.Errorf("unsupported config value %T", v)
				}
			}
			table.Rows = append(table.Rows, cells)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
		out = append(out, table)
	}
	return out, nil
}
func restoreTables(ctx context.Context, tx *sql.Tx, tables []tableDump) error {
	allowed := map[string]bool{}
	for _, name := range append(append([]string{}, configTables...), runtimeTables...) {
		allowed[name] = true
	}
	for i := len(tables) - 1; i >= 0; i-- {
		if !allowed[tables[i].Name] {
			return errors.New("备份包含未知配置表")
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM `+quoteColumn(tables[i].Name)); err != nil {
			return err
		}
	}
	for _, table := range tables {
		rows, err := tx.QueryContext(ctx, `PRAGMA table_info(`+quoteColumn(table.Name)+`)`)
		if err != nil {
			return err
		}
		columns := map[string]bool{}
		for rows.Next() {
			var cid, notnull, pk int
			var name, typ string
			var def any
			if err = rows.Scan(&cid, &name, &typ, &notnull, &def, &pk); err != nil {
				rows.Close()
				return err
			}
			columns[name] = true
		}
		rows.Close()
		quoted := []string{}
		placeholders := []string{}
		seen := map[string]bool{}
		for _, col := range table.Columns {
			if !columns[col] || seen[col] {
				return errors.New("备份字段与当前版本不兼容")
			}
			seen[col] = true
			quoted = append(quoted, quoteColumn(col))
			placeholders = append(placeholders, "?")
		}
		if len(quoted) == 0 {
			return errors.New("备份表缺少字段")
		}
		query := `INSERT INTO ` + quoteColumn(table.Name) + `(` + strings.Join(quoted, ",") + `) VALUES(` + strings.Join(placeholders, ",") + `)`
		for _, row := range table.Rows {
			if len(row) != len(table.Columns) {
				return errors.New("备份行字段数量无效")
			}
			values := make([]any, len(row))
			for i, cell := range row {
				switch cell.Type {
				case "null":
					values[i] = nil
				case "text":
					values[i] = cell.Value
				case "blob":
					values[i], err = base64.StdEncoding.DecodeString(cell.Value)
				case "integer":
					values[i], err = strconv.ParseInt(cell.Value, 10, 64)
				case "real":
					values[i], err = strconv.ParseFloat(cell.Value, 64)
				default:
					return errors.New("备份值类型无效")
				}
				if err != nil {
					return err
				}
			}
			if _, err = tx.ExecContext(ctx, query, values...); err != nil {
				return err
			}
		}
	}
	return nil
}
func (a *App) captureConfiguration(ctx context.Context, prefs map[string]string) (configPayload, error) {
	payload := configPayload{Format: configFormat, Created: time.Now().UTC().Format(time.RFC3339), SourceDir: a.dataDir, Key: a.masterKey, Preferences: prefs, Files: []configFile{}}
	tx, err := a.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return payload, err
	}
	defer tx.Rollback()
	payload.Tables, err = captureTables(ctx, tx, configTables)
	stripTransientSettings(payload.Tables)
	if err != nil {
		return payload, err
	}
	if err = tx.Commit(); err != nil {
		return payload, err
	}
	total := 0
	for _, dir := range []string{"custom-certs", ".local/share/caddy", ".config/caddy"} {
		root := filepath.Join(a.dataDir, filepath.FromSlash(dir))
		if _, e := os.Stat(root); errors.Is(e, os.ErrNotExist) {
			continue
		}
		err = filepath.WalkDir(root, func(path string, d os.DirEntry, e error) error {
			if e != nil {
				return e
			}
			if d.Type()&os.ModeSymlink != 0 {
				return errors.New("配置目录中包含符号链接，无法导出")
			}
			if d.IsDir() {
				return nil
			}
			raw, e := os.ReadFile(path)
			if e != nil {
				return e
			}
			total += len(raw)
			if total > 24<<20 || len(payload.Files) >= 2000 {
				return errors.New("配置证书文件超过备份大小限制")
			}
			rel, e := filepath.Rel(a.dataDir, path)
			if e != nil {
				return e
			}
			payload.Files = append(payload.Files, configFile{Path: filepath.ToSlash(rel), Data: raw})
			return nil
		})
		if err != nil {
			return payload, err
		}
	}
	return payload, nil
}

// sealConfiguration encrypts the whole archive with a key derived from the
// backup password. The portable key that wraps DNS credentials lives inside the
// ciphertext, so neither credentials nor the certificate private keys in Files
// can be read from the exported file without the password.
func sealConfiguration(payload configPayload, password string) (configEnvelope, error) {
	if len(password) < archiveMinPassword || len(password) > 1024 {
		return configEnvelope{}, fmt.Errorf("备份密码至少需要 %d 个字符", archiveMinPassword)
	}
	portable := make([]byte, 32)
	if _, err := rand.Read(portable); err != nil {
		return configEnvelope{}, err
	}
	var err error
	payload, err = jsonCopyPayload(payload)
	if err != nil {
		return configEnvelope{}, err
	}
	if err = rewrapArchiveCredentials(&payload, portable); err != nil {
		return configEnvelope{}, err
	}
	payload.Key = portable
	plain, err := json.Marshal(payload)
	if err != nil {
		return configEnvelope{}, err
	}
	salt := make([]byte, 16)
	if _, err = rand.Read(salt); err != nil {
		return configEnvelope{}, err
	}
	kek, err := scrypt.Key([]byte(password), salt, archiveScryptN, archiveScryptR, archiveScryptP, 32)
	if err != nil {
		return configEnvelope{}, err
	}
	nonce, ciphertext, err := secretstore.Encrypt(kek, plain, []byte(archiveAAD))
	if err != nil {
		return configEnvelope{}, err
	}
	return configEnvelope{Format: configFormat, Created: payload.Created, Encrypted: true, KDF: "scrypt", Salt: salt, Nonce: nonce, Ciphertext: ciphertext}, nil
}

// sealRecoverySnapshot writes the server-side rollback copy taken before an
// import. It never leaves dataDir, which already holds the master key and the
// certificate private keys, so it stays password-free to remain recoverable
// without the panel.
func sealRecoverySnapshot(payload configPayload) (configEnvelope, error) {
	portable := make([]byte, 32)
	if _, err := rand.Read(portable); err != nil {
		return configEnvelope{}, err
	}
	var err error
	payload, err = jsonCopyPayload(payload)
	if err != nil {
		return configEnvelope{}, err
	}
	if err = rewrapArchiveCredentials(&payload, portable); err != nil {
		return configEnvelope{}, err
	}
	payload.Key = portable
	return configEnvelope{Format: configFormat, Created: payload.Created, Payload: &payload}, nil
}
func decryptArchivePayload(envelope configEnvelope, password string) ([]byte, error) {
	if envelope.KDF != "scrypt" || len(envelope.Salt) < 16 || len(envelope.Nonce) == 0 || len(envelope.Ciphertext) == 0 {
		return nil, errors.New("不支持的备份加密格式")
	}
	kek, err := scrypt.Key([]byte(password), envelope.Salt, archiveScryptN, archiveScryptR, archiveScryptP, 32)
	if err != nil {
		return nil, err
	}
	return secretstore.Decrypt(kek, envelope.Nonce, envelope.Ciphertext, []byte(archiveAAD))
}

func jsonCopyPayload(p configPayload) (configPayload, error) {
	var out configPayload
	b, e := json.Marshal(p)
	if e == nil {
		e = json.Unmarshal(b, &out)
	}
	return out, e
}
func rewrapArchiveCredentials(payload *configPayload, destination []byte) error {
	for ti := range payload.Tables {
		table := &payload.Tables[ti]
		if table.Name != "dns_providers" {
			continue
		}
		cols := map[string]int{}
		for i, col := range table.Columns {
			cols[col] = i
		}
		for _, row := range table.Rows {
			if len(row) != len(table.Columns) {
				return errors.New("Invalid credential row")
			}
			id := row[cols["id"]].Value
			n, e := base64.StdEncoding.DecodeString(row[cols["credentials_nonce"]].Value)
			if e != nil {
				return e
			}
			c, e := base64.StdEncoding.DecodeString(row[cols["credentials_ciphertext"]].Value)
			if e != nil {
				return e
			}
			aad := []byte("viya:dns-provider:" + id + ":v1")
			plain, e := secretstore.Decrypt(payload.Key, n, c, aad)
			if e != nil {
				return e
			}
			n, c, e = secretstore.Encrypt(destination, plain, aad)
			if e != nil {
				return e
			}
			row[cols["credentials_nonce"]] = configCell{Type: "blob", Value: base64.StdEncoding.EncodeToString(n)}
			row[cols["credentials_ciphertext"]] = configCell{Type: "blob", Value: base64.StdEncoding.EncodeToString(c)}
		}
	}
	return nil
}
func openConfiguration(input configImportInput) (configPayload, error) {
	var payload configPayload
	if input.Archive.Format != configFormat {
		return payload, errors.New("请使用新版不含账户信息的 Viya 配置文件")
	}
	if input.Archive.Encrypted {
		if len(input.Password) < archiveMinPassword {
			return payload, errors.New("该备份已加密，请输入备份密码")
		}
		plain, err := decryptArchivePayload(input.Archive, input.Password)
		if err != nil {
			return payload, errors.New("备份密码不正确，或文件已损坏")
		}
		if err = json.Unmarshal(plain, &payload); err != nil {
			return payload, errors.New("备份内容无法解析")
		}
	} else if input.Archive.Payload != nil {
		// Legacy password-free archive produced before backups were encrypted.
		payload = *input.Archive.Payload
	} else {
		return payload, errors.New("备份缺少配置内容")
	}
	if payload.Format != configFormat || len(payload.Key) != 32 || len(payload.Tables) != len(configTables) {
		return payload, errors.New("配置版本或表数量不兼容")
	}
	for i, name := range configTables {
		table := payload.Tables[i]
		if table.Name != name {
			return payload, errors.New("备份配置表顺序或内容不兼容")
		}
		if len(table.Columns) == 0 {
			return payload, errors.New("备份缺少字段")
		}
		seenCols := map[string]bool{}
		for _, col := range table.Columns {
			if seenCols[col] {
				return payload, errors.New("备份字段重复")
			}
			seenCols[col] = true
		}
		for _, row := range table.Rows {
			if len(row) != len(table.Columns) {
				return payload, errors.New("备份行无效")
			}
		}
		if name == "dns_providers" {
			for _, col := range []string{"id", "credentials_nonce", "credentials_ciphertext"} {
				if !seenCols[col] {
					return payload, errors.New("备份缺少凭据字段")
				}
			}
		}
	}
	seen := map[string]bool{}
	size := 0
	for _, file := range payload.Files {
		clean := filepath.Clean(filepath.FromSlash(file.Path))
		if filepath.IsAbs(clean) || strings.Contains(clean, ":") || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || !allowedConfigFile(filepath.ToSlash(clean)) || seen[clean] {
			return payload, errors.New("备份文件路径无效")
		}
		seen[clean] = true
		size += len(file.Data)
	}
	if size > 24<<20 || len(payload.Files) > 2000 {
		return payload, errors.New("备份文件过大")
	}
	return payload, nil
}
func allowedConfigFile(path string) bool {
	return strings.HasPrefix(path, "custom-certs/") || strings.HasPrefix(path, ".local/share/caddy/") || strings.HasPrefix(path, ".config/caddy/")
}
func (a *App) exportConfiguration(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Preferences map[string]string `json:"preferences"`
		Password    string            `json:"password"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&input) != nil {
		fail(w, 400, errors.New("导出参数无效"))
		return
	}
	if len(input.Password) < archiveMinPassword || len(input.Password) > 1024 {
		fail(w, http.StatusBadRequest, fmt.Errorf("请设置至少 %d 位的备份密码，用于加密 DNS 凭据与证书私钥", archiveMinPassword))
		return
	}
	a.listenerMu.Lock()
	defer a.listenerMu.Unlock()
	a.ddnsMu.Lock()
	defer a.ddnsMu.Unlock()
	a.mu.Lock()
	defer a.mu.Unlock()
	payload, err := a.captureConfiguration(r.Context(), input.Preferences)
	if err != nil {
		fail(w, 500, errors.New("无法读取完整配置"))
		return
	}
	archive, err := sealConfiguration(payload, input.Password)
	if err != nil {
		fail(w, 400, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Disposition", `attachment; filename="viya-config-`+time.Now().UTC().Format("20060102T150405")+`.json"`)
	writeJSON(w, 200, archive)
}
func (a *App) inspectConfiguration(w http.ResponseWriter, r *http.Request) {
	var input configImportInput
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 80<<20)).Decode(&input) != nil {
		fail(w, 400, errors.New("备份格式无效或文件过大"))
		return
	}
	payload, err := openConfiguration(input)
	if err != nil {
		fail(w, 400, err)
		return
	}
	counts := map[string]int{}
	for _, table := range payload.Tables {
		counts[table.Name] = len(table.Rows)
	}
	writeJSON(w, 200, map[string]any{"created": payload.Created, "counts": counts, "files": len(payload.Files), "preferences": len(payload.Preferences), "requires_login": false, "encrypted": input.Archive.Encrypted})
}
func (a *App) importConfiguration(w http.ResponseWriter, r *http.Request) {
	var input configImportInput
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 80<<20)).Decode(&input) != nil || !input.Confirmed {
		fail(w, 400, errors.New("请预览备份并明确确认替换所有配置"))
		return
	}
	payload, err := openConfiguration(input)
	if err != nil {
		fail(w, 400, err)
		return
	}
	a.configMu.Lock()
	defer a.configMu.Unlock()
	secret, ok := sessionCookie(r)
	if !ok {
		fail(w, 401, errors.New("登录已失效"))
		return
	}
	token := sha256.Sum256([]byte(secret))
	var valid int
	if a.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM sessions WHERE token_hash=? AND expires_at>?`, token[:], time.Now().Unix()).Scan(&valid) != nil || valid != 1 {
		fail(w, 401, errors.New("登录已失效，请重新登录"))
		return
	}
	a.listenerMu.Lock()
	defer a.listenerMu.Unlock()
	a.ddnsMu.Lock()
	defer a.ddnsMu.Unlock()
	a.mu.Lock()
	defer a.mu.Unlock()
	// Keep a restricted recovery snapshot before replacement; accounts and sessions remain untouched.
	before, err := a.captureConfiguration(r.Context(), nil)
	if err != nil {
		fail(w, 500, errors.New("无法创建导入前备份"))
		return
	}
	runtime, err := captureTables(r.Context(), a.db, runtimeTables)
	if err != nil {
		fail(w, 500, err)
		return
	}
	archive, err := sealRecoverySnapshot(before)
	if err != nil {
		fail(w, 500, err)
		return
	}
	backupDir := filepath.Join(a.dataDir, "backups")
	if err = os.MkdirAll(backupDir, 0700); err != nil {
		fail(w, 500, err)
		return
	}
	backupPath := filepath.Join(backupDir, "before-import-"+time.Now().UTC().Format("20060102T150405.000000000")+".json")
	raw, _ := json.Marshal(archive)
	if err = os.WriteFile(backupPath, raw, 0600); err != nil {
		fail(w, 500, err)
		return
	}
	// Credential ciphertext is re-encrypted for this installation's unchanged master key.
	for t := range payload.Tables {
		table := &payload.Tables[t]
		if table.Name == "dns_providers" {
			columns := map[string]int{}
			for i, col := range table.Columns {
				columns[col] = i
			}
			for _, row := range table.Rows {
				id := row[columns["id"]].Value
				nonce, e := base64.StdEncoding.DecodeString(row[columns["credentials_nonce"]].Value)
				if e != nil {
					fail(w, 400, errors.New("凭据数据无效"))
					return
				}
				ct, e := base64.StdEncoding.DecodeString(row[columns["credentials_ciphertext"]].Value)
				if e != nil {
					fail(w, 400, e)
					return
				}
				aad := []byte("viya:dns-provider:" + id + ":v1")
				plain, e := secretstore.Decrypt(payload.Key, nonce, ct, aad)
				if e != nil {
					fail(w, 400, errors.New("无法解密备份 DNS 凭据"))
					return
				}
				n, c, e := secretstore.Encrypt(a.masterKey, plain, aad)
				if e != nil {
					fail(w, 500, e)
					return
				}
				row[columns["credentials_nonce"]] = configCell{Type: "blob", Value: base64.StdEncoding.EncodeToString(n)}
				row[columns["credentials_ciphertext"]] = configCell{Type: "blob", Value: base64.StdEncoding.EncodeToString(c)}
			}
		}
	}
	// Certificate files are written once, straight into the data directory
	// below; staging them first duplicated up to 24 MiB of writes for nothing.
	for t := range payload.Tables {
		if payload.Tables[t].Name == "custom_certificates" {
			for _, row := range payload.Tables[t].Rows {
				for i, col := range payload.Tables[t].Columns {
					if col == "cert_path" || col == "key_path" {
						old := strings.ReplaceAll(row[i].Value, "\\", "/")
						prefix := strings.TrimRight(strings.ReplaceAll(payload.SourceDir, "\\", "/"), "/") + "/"
						if !strings.HasPrefix(old, prefix) {
							fail(w, 400, errors.New("证书路径不属于源数据目录"))
							return
						}
						rel := strings.TrimPrefix(old, prefix)
						rel = filepath.ToSlash(filepath.Clean(filepath.FromSlash(rel)))
						if !allowedConfigFile(rel) {
							fail(w, 400, errors.New("证书路径无效"))
							return
						}
						found := false
						for _, file := range payload.Files {
							if filepath.ToSlash(filepath.Clean(filepath.FromSlash(file.Path))) == rel {
								found = true
								break
							}
						}
						if !found {
							fail(w, 400, errors.New("备份缺少引用的证书文件"))
							return
						}
						row[i].Value = filepath.Join(a.dataDir, filepath.FromSlash(rel))
					}
				}
			}
		}
	}
	recoveryCtx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 90*time.Second)
	defer cancel()
	applyTables := func(tables []tableDump) error {
		tx, e := a.db.BeginTx(recoveryCtx, nil)
		if e != nil {
			return e
		}
		defer tx.Rollback()
		if e = restoreTables(recoveryCtx, tx, tables); e != nil {
			return e
		}
		return tx.Commit()
	}
	stripTransientSettings(payload.Tables)
	if err = applyTables(payload.Tables); err == nil {
		for _, file := range payload.Files {
			dest := filepath.Join(a.dataDir, filepath.FromSlash(file.Path))
			if e := os.MkdirAll(filepath.Dir(dest), 0700); e != nil {
				err = e
				break
			}
			if e := os.WriteFile(dest, file.Data, 0600); e != nil {
				err = e
				break
			}
		}
	}
	if err == nil {
		var imported []WebListener
		imported, err = a.webListeners(recoveryCtx)
		if err == nil {
			for _, listener := range imported {
				if err = a.listenerPortAvailable(recoveryCtx, listener.Port, listener.Protocol); err != nil {
					break
				}
			}
		}
		if err == nil {
			err = a.validateRestoredBindings(recoveryCtx)
		}
	}
	if err == nil {
		err = a.applyLocked(recoveryCtx)
	}
	if err != nil {
		restoreErr := applyTables(append(before.Tables, runtime...))
		knownFiles := map[string]bool{}
		for _, file := range before.Files {
			knownFiles[filepath.ToSlash(filepath.Clean(filepath.FromSlash(file.Path)))] = true
		}
		for _, file := range payload.Files {
			clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(file.Path)))
			if !knownFiles[clean] {
				_ = os.Remove(filepath.Join(a.dataDir, filepath.FromSlash(clean)))
			}
		}
		for _, file := range before.Files {
			dest := filepath.Join(a.dataDir, filepath.FromSlash(file.Path))
			_ = os.MkdirAll(filepath.Dir(dest), 0700)
			_ = os.WriteFile(dest, file.Data, 0600)
		}
		engineErr := a.applyLocked(recoveryCtx)
		if restoreErr != nil || engineErr != nil {
			fail(w, 500, errors.New("导入失败，自动恢复未完全完成；恢复备份位于服务器 backups 目录"))
			return
		}
		fail(w, 400, errors.New("导入配置未通过校验或引擎应用，已恢复导入前配置"))
		return
	}
	a.configureProgramLogging()
	writeJSON(w, 200, map[string]any{"ok": true, "preferences": payload.Preferences, "requires_login": false, "message": "业务配置已恢复，当前账号和登录保持不变；面板端口在下次重启后生效"})
}

func (a *App) validateRestoredBindings(ctx context.Context) error {
	rows, err := a.db.QueryContext(ctx, `SELECT id,domain,listener_id,upstream,tls_mode,dns_provider_id,custom_certificate_id,type,root_path,redirect_target,redirect_status FROM proxy_hosts`)
	if err != nil {
		return err
	}
	hosts := []Host{}
	for rows.Next() {
		var h Host
		if err = rows.Scan(&h.ID, &h.Domain, &h.ListenerID, &h.Upstream, &h.TLSMode, &h.DNSProviderID, &h.CustomCertificateID, &h.Type, &h.RootPath, &h.RedirectTarget, &h.RedirectStatus); err != nil {
			rows.Close()
			return err
		}
		hosts = append(hosts, h)
	}
	rows.Close()
	for _, h := range hosts {
		if err = validateHost(&h); err != nil {
			return err
		}
		if err = a.validateHostListener(ctx, &h); err != nil {
			return err
		}
		if err = a.validateTLSSettings(ctx, &h); err != nil {
			return err
		}
	}
	return nil
}

// A port-confirmation deadline belongs to a running process, not a portable configuration.
func stripTransientSettings(tables []tableDump) {
	for i := range tables {
		if tables[i].Name != "panel_settings" {
			continue
		}
		column := -1
		for j, name := range tables[i].Columns {
			if name == "key" {
				column = j
				break
			}
		}
		if column < 0 {
			continue
		}
		kept := [][]configCell{}
		for _, row := range tables[i].Rows {
			if row[column].Value != "pending_panel_port" {
				kept = append(kept, row)
			}
		}
		tables[i].Rows = kept
	}
}
