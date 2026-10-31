package main

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
)

// migrateLegacyDatabaseFilename supports local installations created before
// the project-wide rename. The installer performs the same migration using
// SQLite's backup API after stopping the service.
func migrateLegacyDatabaseFilename(dataDir string) error {
	current := filepath.Join(dataDir, "viya.db")
	legacy := filepath.Join(dataDir, "crpm.db")
	if _, err := os.Stat(current); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if _, err := os.Stat(legacy); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}

	db, err := sql.Open("sqlite", legacy)
	if err != nil {
		return err
	}
	_, checkpointErr := db.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`)
	closeErr := db.Close()
	if checkpointErr != nil {
		return checkpointErr
	}
	if closeErr != nil {
		return closeErr
	}
	if err := os.Rename(legacy, current); err != nil {
		return err
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		_ = os.Remove(legacy + suffix)
	}
	return nil
}
