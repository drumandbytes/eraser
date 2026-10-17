//go:build !windows

package main

import (
	"os"
	"syscall"
	"testing"
)

func requireInterrupt(t *testing.T) { t.Helper() }

// interruptSelf is the Ctrl+C a user would press.
func interruptSelf(t *testing.T) {
	t.Helper()
	if err := syscall.Kill(os.Getpid(), syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
}
