package main

import (
	"context"
	"path/filepath"
	"runtime"
	"strings"
)

func pathOverlaps(root, candidate string) bool {
	rel, err := filepath.Rel(root, candidate)
	if err == nil && (rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))) {
		return true
	}
	if runtime.GOOS == "windows" && strings.EqualFold(filepath.Clean(root), filepath.Clean(candidate)) {
		return true
	}
	return false
}

// Execute only after a confirmed engine load. A failed delete must never
// remove files that the previous, still-running Caddy configuration serves.
func (a *App) cleanupDeletedSites(ctx context.Context) error {
	rows, err := a.db.QueryContext(ctx, `SELECT root_path FROM pending_file_deletions`)
	if err != nil {
		return err
	}
	var pending []string
	for rows.Next() {
		var root string
		if err = rows.Scan(&root); err != nil {
			rows.Close()
			return err
		}
		pending = append(pending, root)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, root := range pending {
		rows, err = a.db.QueryContext(ctx, `SELECT domain,root_path FROM proxy_hosts WHERE type IN ('static','files')`)
		if err != nil {
			return err
		}
		used := false
		for rows.Next() {
			var domain, path string
			if err = rows.Scan(&domain, &path); err != nil {
				rows.Close()
				return err
			}
			if path == "" {
				path = managedStaticSiteRoot(a, domain)
			}
			if pathOverlaps(root, path) {
				used = true
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		if !used {
			if err = a.removeManagedStaticSite(root); err != nil {
				return err
			}
		}
		if _, err = a.db.ExecContext(ctx, `DELETE FROM pending_file_deletions WHERE root_path=?`, root); err != nil {
			return err
		}
	}
	return nil
}
