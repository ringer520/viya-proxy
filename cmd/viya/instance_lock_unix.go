//go:build !windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// acquireInstanceLock keeps two processes from opening the same data
// directory. flock is released by the kernel when the process exits, so a
// crash cannot leave a stale lock behind. Different data directories are still
// allowed on purpose (tests, staged installs); startup logs both the directory
// and the listen address so a split panel is visible immediately.
func acquireInstanceLock(dataDir string) (func(), error) {
	path := filepath.Join(dataDir, "viya.lock")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("open instance lock %s: %w", path, err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, fmt.Errorf("another Viya process already uses data directory %s (lock %s)", dataDir, path)
	}
	if err := f.Truncate(0); err == nil {
		fmt.Fprintf(f, "pid=%d\n", os.Getpid())
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}, nil
}
