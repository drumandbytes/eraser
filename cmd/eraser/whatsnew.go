package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/drumandbytes/eraser/internal/config"
)

// announceTwoPointZero prints a one-time pointer to the 2.0 upgrade guide the
// first time an existing install runs a command. `serve` is skipped (its web
// UI shows the notice) and so are commands that don't touch the config.
func announceTwoPointZero(cmd *cobra.Command) {
	switch cmd.Name() {
	case "serve", "init", "help", "completion", "version", "guides", "validate-brokers", "list-brokers":
		return
	}
	path := resolveConfigPath()
	if _, err := os.Stat(path); err != nil || config.WhatsNewSeen(path) {
		return
	}
	config.MarkWhatsNewSeen(path)
	fmt.Fprintln(os.Stderr, "Eraser 2.0: country is now an ISO code, mail passwords are encrypted (optional passphrase: `eraser passphrase set`), and some old settings are gone. Details: https://github.com/drumandbytes/eraser/blob/main/docs/upgrading-to-2.0.md")
}
