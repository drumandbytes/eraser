//go:build !race

// go-imap v1.2.1 writes IDLE's DONE from its own goroutine; -race flags it
// (upstream), so this only runs without the race detector.

package main

import (
	"os"
	"strings"
	"syscall"
	"testing"
	"time"
)

// --watch keeps every inbox open until Ctrl+C, then exits cleanly.
func TestMonitorWatchUntilInterrupted(t *testing.T) {
	e := newCLIEnv(t, manualConfig+imapInbox(t, [3]string{"privacy@acme.example", "Re: request", "We deleted your data."}))
	done := make(chan error, 1)
	var out string
	go func() {
		var err error
		out, err = e.run(t, "", "monitor", "--watch")
		done <- err
	}()
	// The SIGINT handler is in place from the start; give the scan time to
	// reach IDLE first.
	time.Sleep(2 * time.Second)
	if err := syscall.Kill(os.Getpid(), syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("monitor --watch didn't stop on SIGINT")
	}
	if !strings.Contains(out, "Watching username for new emails") {
		t.Errorf("output:\n%s", out)
	}
}
