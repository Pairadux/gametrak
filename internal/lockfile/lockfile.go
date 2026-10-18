// Package lockfile provides exclusive advisory locks shared between the daemon
// and other gametrak commands running at the same time.
package lockfile

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// Acquire blocks until it holds an exclusive lock on path, creating the file
// if needed, and returns the function that releases it. Locks are per open
// file, so two goroutines in one process exclude each other too.
func Acquire(path string) (release func(), err error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, fmt.Errorf("failed to create lock directory: %w", err)
	}
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open lock file: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, fmt.Errorf("failed to lock %s: %w", path, err)
	}
	return func() {
		syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
	}, nil
}
