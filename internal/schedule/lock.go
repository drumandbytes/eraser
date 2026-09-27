// Package schedule runs Eraser's send + inbox cycle unattended: the OS
// scheduler (launchd, systemd) when installed, otherwise an in-process loop.
// All modes share one lock so two cycles never overlap.
package schedule

import (
	"fmt"
	"os"
	"path/filepath"
)

// TryLock takes the cycle lock in dir (the config directory). ok is false
// when another cycle holds it. The OS releases the lock if the process
// dies, so there are no stale lock files to clean up.
func TryLock(dir string) (release func(), ok bool, err error) {
	f, err := os.OpenFile(filepath.Join(dir, "auto.lock"), os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		return nil, false, fmt.Errorf("failed to open lock file: %w", err)
	}
	ok, err = tryLockFile(f)
	if err != nil || !ok {
		_ = f.Close()
		if err != nil {
			return nil, false, fmt.Errorf("failed to take lock: %w", err)
		}
		return nil, false, nil
	}
	// Closing the file releases the lock.
	return func() { _ = f.Close() }, true, nil
}
