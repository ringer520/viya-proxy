//go:build windows

package main

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

// acquireInstanceLock mirrors the Unix flock behaviour with a kernel lock.
// Windows releases the lock when the handle closes or the process exits, so
// there is no stale pid file to clean up after a crash.
func acquireInstanceLock(dataDir string) (func(), error) {
	path := filepath.Join(dataDir, "viya.lock")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("open instance lock %s: %w", path, err)
	}
	handle := windows.Handle(f.Fd())
	ol := new(windows.Overlapped)
	flags := uint32(windows.LOCKFILE_EXCLUSIVE_LOCK | windows.LOCKFILE_FAIL_IMMEDIATELY)
	if err := windows.LockFileEx(handle, flags, 0, 1, 0, ol); err != nil {
		f.Close()
		return nil, fmt.Errorf("another Viya process already uses data directory %s (lock %s)", dataDir, path)
	}
	if err := f.Truncate(0); err == nil {
		fmt.Fprintf(f, "pid=%d\n", os.Getpid())
	}
	return func() {
		_ = windows.UnlockFileEx(handle, 0, 1, 0, ol)
		_ = f.Close()
	}, nil
}
