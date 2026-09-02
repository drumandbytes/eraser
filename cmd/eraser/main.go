package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/drumandbytes/eraser/internal/broker"
	"github.com/drumandbytes/eraser/internal/config"
	"github.com/spf13/cobra"
)

var (
	cfgFile     string
	brokerFile  string
	profileFlag string
)

// version is overridden at release time via -ldflags "-X main.version=...".
var version = "dev"

// resolveProfile resolves which configured profile a command should operate
// as, honoring the global --profile flag. With the common single-profile
// setup, --profile can be omitted entirely - GetProfile falls back to the
// sole configured profile. With multiple profiles configured, --profile is
// required and GetProfile returns an error listing the available IDs.
func resolveProfile(cfg *config.Config) (config.NamedProfile, error) {
	return cfg.GetProfile(profileFlag)
}

// resolveBrokerWritePath returns a filesystem path for the few commands that
// modify the broker database (add-broker, cleanup-bounces). Read-only commands
// use broker.Load(brokerFile), which also has an embedded fallback. Order:
// --brokers, then a local ./data/brokers.yaml checkout, then the per-user copy.
func resolveBrokerWritePath() string {
	if brokerFile != "" {
		return brokerFile
	}
	if _, err := os.Stat("data/brokers.yaml"); err == nil {
		return "data/brokers.yaml"
	}
	return broker.UserBrokersPath()
}

func resolveConfigPath() string {
	if cfgFile != "" {
		return cfgFile
	}
	return config.DefaultConfigPath()
}

func main() {
	rootCmd := &cobra.Command{
		Use:     "eraser",
		Version: version,
		Short:   "Eraser - Automated data broker removal requests",
		Long: `Eraser is an open-source tool that automates sending data removal
requests to data brokers, helping you protect your privacy.

It supports GDPR, CCPA, and generic removal request templates, and can
send via Gmail SMTP.`,
	}
	// So a standalone binary (no LICENSE file alongside it) still points at
	// the MIT terms it ships under.
	rootCmd.SetVersionTemplate("eraser {{.Version}}\nMIT licensed - https://github.com/drumandbytes/eraser/blob/main/LICENSE\n")

	// Global flags
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.eraser/config.yaml)")
	rootCmd.PersistentFlags().StringVar(&brokerFile, "brokers", "", "broker database file (default is ./data/brokers.yaml)")
	rootCmd.PersistentFlags().StringVar(&profileFlag, "profile", "", "Profile ID to operate as (default: the only configured profile; required if you've configured more than one via 'eraser profile add')")

	// Add commands
	rootCmd.AddCommand(initCmd())
	rootCmd.AddCommand(sendCmd())
	rootCmd.AddCommand(serveCmd())
	rootCmd.AddCommand(listBrokersCmd())
	rootCmd.AddCommand(statusCmd())
	rootCmd.AddCommand(addBrokerCmd())
	rootCmd.AddCommand(monitorCmd())
	rootCmd.AddCommand(pipelineCmd())
	rootCmd.AddCommand(fillCmd())
	rootCmd.AddCommand(confirmCmd())
	rootCmd.AddCommand(cleanupBouncesCmd())
	rootCmd.AddCommand(markBouncedCmd())
	rootCmd.AddCommand(auditBrokersCmd())
	rootCmd.AddCommand(profileCmd())
	rootCmd.AddCommand(exportCmd())
	rootCmd.AddCommand(draftCmd())
	rootCmd.AddCommand(markSentCmd())
	rootCmd.AddCommand(updateBrokersCmd())
	rootCmd.AddCommand(validateBrokersCmd())
	rootCmd.AddCommand(guidesCmd())

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func prompt(reader *bufio.Reader, message string) string {
	fmt.Print(message)
	input, err := reader.ReadString('\n')
	if err != nil {
		return ""
	}
	return strings.TrimSpace(input)
}
