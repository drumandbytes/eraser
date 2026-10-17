//go:build windows

package main

import "testing"

// Windows has no way to raise Ctrl+C on the test process itself: there is no
// syscall.Kill, and a console control event needs a console the CI runner lacks.
func requireInterrupt(t *testing.T) {
	t.Helper()
	t.Skip("cannot send Ctrl+C to the test process on Windows")
}

func interruptSelf(t *testing.T) {
	t.Helper()
	t.Fatal("unreachable: requireInterrupt skips on Windows")
}
