package main

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/drumandbytes/delistd/internal/config"
)

func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	r, w, _ := os.Pipe()
	old := os.Stderr
	os.Stderr = w
	fn()
	_ = w.Close()
	os.Stderr = old
	out, _ := io.ReadAll(r)
	return string(out)
}

func TestInvokedAsEraser(t *testing.T) {
	old := os.Args
	defer func() { os.Args = old }()
	for arg, want := range map[string]bool{
		"/opt/homebrew/bin/eraser":     true,
		"eraser.exe":                   true,
		"/usr/local/bin/delistd":       false,
		"/usr/local/bin/eraser-helper": false,
	} {
		os.Args = []string{arg}
		if got := invokedAsEraser(); got != want {
			t.Errorf("invokedAsEraser(%q) = %v, want %v", arg, got, want)
		}
	}
}

func TestRenameAnnouncementShowsOnce(t *testing.T) {
	e := newCLIEnv(t, manualConfig)
	if config.RenameNoticeSeen(e.cfgPath) {
		t.Fatal("setup of the test env should leave it unseen")
	}
	out := captureStderr(t, func() {
		if _, err := e.run(t, "", "status", "--profile", "default"); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "Eraser is now Delistd") || !config.RenameNoticeSeen(e.cfgPath) {
		t.Errorf("first command should announce the rename and remember it: %q", out)
	}
	out = captureStderr(t, func() {
		if _, err := e.run(t, "", "status", "--profile", "default"); err != nil {
			t.Fatal(err)
		}
	})
	if strings.Contains(out, "Eraser is now Delistd") {
		t.Errorf("announced twice: %q", out)
	}
}

func TestRenameAnnouncementThroughTheOldNameRepeats(t *testing.T) {
	old := os.Args
	defer func() { os.Args = old }()
	os.Args = []string{"/opt/homebrew/bin/eraser"}
	e := newCLIEnv(t, manualConfig)
	for i := 0; i < 2; i++ {
		out := captureStderr(t, func() {
			if _, err := e.run(t, "", "status", "--profile", "default"); err != nil {
				t.Fatal(err)
			}
		})
		if !strings.Contains(out, "run `delistd` instead") {
			t.Errorf("run %d: alias notice missing: %q", i, out)
		}
	}
}

func TestQuietCommands(t *testing.T) {
	root := newRootCmd()
	for _, name := range []string{"serve", "init", "mcp", "list-brokers"} {
		c, _, err := root.Find([]string{name})
		if err != nil || !quietCommand(c) {
			t.Errorf("%s should be quiet (err=%v)", name, err)
		}
	}
	c, _, _ := root.Find([]string{"auto"})
	if quietCommand(c) {
		t.Error("auto runs the scheduled cycle, so it must be allowed to migrate the schedule")
	}
}

func TestMigrateScheduleSaysNothingWithNothingInstalled(t *testing.T) {
	e := newCLIEnv(t, manualConfig) // a temp HOME with no OS schedule in it
	c, _, _ := newRootCmd().Find([]string{"status"})
	out := captureStderr(t, func() { migrateSchedule(c) })
	if out != "" {
		t.Errorf("unexpected output with nothing to migrate: %q", out)
	}
	_ = e
}
