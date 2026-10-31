package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

func (a *App) initListenerSites() error {
	if err := ensureColumn(a.db, "proxy_hosts", "listener_id", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	var ddl string
	if err := a.db.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name='proxy_hosts'`).Scan(&ddl); err != nil {
		return err
	}
	if strings.Contains(ddl, "domain TEXT NOT NULL UNIQUE") {
		if _, err := a.db.Exec(`PRAGMA foreign_keys=OFF`); err != nil {
			return err
		}
		defer a.db.Exec(`PRAGMA foreign_keys=ON`)
		tx, err := a.db.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()
		next := strings.Replace(ddl, "proxy_hosts", "proxy_hosts_next", 1)
		next = strings.Replace(next, "domain TEXT NOT NULL UNIQUE", "domain TEXT NOT NULL", 1)
		at := strings.LastIndex(next, ")")
		next = next[:at] + ", UNIQUE(domain,listener_id)" + next[at:]
		if _, err = tx.Exec(next); err != nil {
			return err
		}
		if _, err = tx.Exec(`INSERT INTO proxy_hosts_next SELECT * FROM proxy_hosts; DROP TABLE proxy_hosts; ALTER TABLE proxy_hosts_next RENAME TO proxy_hosts`); err != nil {
			return err
		}
		if err = tx.Commit(); err != nil {
			return err
		}
	}
	listeners, err := a.webListeners(context.Background())
	if err != nil {
		return err
	}
	var legacy int
	if err = a.db.QueryRow(`SELECT COUNT(*) FROM proxy_hosts WHERE listener_id=''`).Scan(&legacy); err != nil {
		return err
	}
	tx, err := a.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, protocol := range []string{"http", "https"} {
		first := ""
		for _, listener := range listeners {
			if listener.Protocol == protocol {
				first = listener.ID
				break
			}
		}
		if first == "" {
			continue
		}
		op := "!="
		if protocol == "http" {
			op = "="
		}
		if _, err = tx.Exec(`UPDATE proxy_hosts SET listener_id=? WHERE listener_id='' AND tls_mode `+op+` 'none'`, first); err != nil {
			return err
		}
	}
	if legacy > 0 {
		for _, protocol := range []string{"http", "https"} {
			first := ""
			for _, listener := range listeners {
				if listener.Protocol != protocol {
					continue
				}
				if first == "" {
					first = listener.ID
					continue
				}
				if err = cloneListenerSites(context.Background(), tx, first, listener.ID, protocol); err != nil {
					return err
				}
			}
		}
	}
	raw, _ := json.Marshal(listeners)
	if _, err = tx.Exec(`INSERT INTO panel_settings(key,value) VALUES('web_listeners',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, string(raw)); err != nil {
		return err
	}
	return tx.Commit()
}
func (a *App) validateHostListener(ctx context.Context, h *Host) error {
	listeners, err := a.webListeners(ctx)
	if err != nil {
		return err
	}
	for _, listener := range listeners {
		matches := h.ListenerID == listener.ID
		if h.ListenerID == "" {
			matches = (h.TLSMode == "none") == (listener.Protocol == "http")
		}
		if !matches {
			continue
		}
		if (h.TLSMode == "none") != (listener.Protocol == "http") {
			return errors.New("站点证书模式与所选 HTTP / HTTPS 监听不一致")
		}
		h.ListenerID = listener.ID
		return nil
	}
	return errors.New("请选择有效的监听")
}
func cloneListenerSites(ctx context.Context, tx *sql.Tx, from, to, protocol string) error {
	rows, err := tx.QueryContext(ctx, `PRAGMA table_info(proxy_hosts)`)
	if err != nil {
		return err
	}
	columns := []string{}
	for rows.Next() {
		var cid, notnull, pk int
		var name, typ string
		var def any
		if err = rows.Scan(&cid, &name, &typ, &notnull, &def, &pk); err != nil {
			rows.Close()
			return err
		}
		if name != "id" {
			columns = append(columns, name)
		}
	}
	if err = rows.Close(); err != nil {
		return err
	}
	selects := make([]string, len(columns))
	for i, col := range columns {
		selects[i] = `"` + col + `"`
		switch col {
		case "listener_id":
			selects[i] = "?"
		case "tls_mode":
			if protocol == "http" {
				selects[i] = "'none'"
			} else {
				selects[i] = "CASE WHEN tls_mode='none' THEN 'auto' ELSE tls_mode END"
			}
		case "dns_provider_id", "custom_certificate_id":
			if protocol == "http" {
				selects[i] = "NULL"
			}
		}
	}
	quoted := make([]string, len(columns))
	for i, col := range columns {
		quoted[i] = `"` + col + `"`
	}
	_, err = tx.ExecContext(ctx, fmt.Sprintf(`INSERT INTO proxy_hosts(%s) SELECT %s FROM proxy_hosts WHERE listener_id=?`, strings.Join(quoted, ","), strings.Join(selects, ",")), to, from)
	return err
}
