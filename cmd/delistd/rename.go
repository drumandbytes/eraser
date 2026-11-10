package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/drumandbytes/delistd/internal/config"
)

// invokedAsEraser reports whether the binary was started through the
// pre-rename `eraser` name (the deprecated alias shipped beside `delistd`).
func invokedAsEraser() bool {
	return strings.TrimSuffix(strings.ToLower(filepath.Base(os.Args[0])), ".exe") == "eraser"
}

// announceRename tells existing users about the rename. Through the old
// `eraser` name it prints on every run, so scripts and scheduled jobs show up in
// their logs; through `delistd` it prints once per install.
func announceRename(cmd *cobra.Command) {
	if invokedAsEraser() {
		fmt.Fprintln(os.Stderr, "Eraser is now Delistd: run `delistd` instead. The `eraser` command still works, but it will be removed in a future major release. Your config and history are unchanged. https://delistd.dev/")
		return
	}
	if !cmd.HasParent() { // bare launch starts the web UI, which shows its own notice
		return
	}
	switch cmd.Name() {
	case "serve", "init", "help", "completion", "version", "guides", "validate-brokers", "list-brokers", "mcp":
		return
	}
	path := resolveConfigPath()
	if _, err := os.Stat(path); err != nil || config.RenameNoticeSeen(path) {
		return
	}
	config.MarkRenameNoticeSeen(path)
	fmt.Fprintln(os.Stderr, "Eraser is now Delistd. The `eraser` command still works for now; use `delistd` from here on. Your config (~/.eraser) and history are unchanged. If you installed an OS schedule, re-run `delistd schedule install`. Details: https://delistd.dev/")
}
