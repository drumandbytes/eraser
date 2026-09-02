package main

import (
	"fmt"

	"github.com/eraser-privacy/eraser/internal/broker"
	"github.com/eraser-privacy/eraser/internal/config"
	"github.com/eraser-privacy/eraser/internal/history"
	"github.com/spf13/cobra"
)

func markSentCmd() *cobra.Command {
	var (
		region   string
		category string
		dryRun   bool
	)

	cmd := &cobra.Command{
		Use:   "mark-sent [broker-id ...]",
		Short: "Record that you sent a removal request by hand",
		Long: `Record one or more brokers as having been sent a removal request that
you sent yourself (from your own mail client), so 'status', 'pipeline' and
'export' account for it. Pairs with 'eraser draft'.

Each broker gets a history row: status "sent", method "manual", dated now.
Re-running is harmless - it just adds another row (same as re-sending).

Examples:
  eraser mark-sent spokeo beenverified
  eraser mark-sent --region eu
  eraser mark-sent --category people-search --dry-run`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMarkSent(args, region, category, dryRun)
		},
	}

	cmd.Flags().StringVar(&region, "region", "", "mark every broker in this region (us, eu, global)")
	cmd.Flags().StringVar(&category, "category", "", "mark every broker in this category")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "list what would be recorded without writing")

	return cmd
}

func runMarkSent(args []string, region, category string, dryRun bool) error {
	if len(args) == 0 && region == "" && category == "" {
		return fmt.Errorf("give one or more broker ids, or a --region / --category filter")
	}

	cfg, err := config.Load(resolveConfigPath())
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	activeProfile, err := resolveProfile(cfg)
	if err != nil {
		return err
	}

	brokerDB, err := broker.Load(brokerFile)
	if err != nil {
		return fmt.Errorf("failed to load brokers: %w", err)
	}
	brokers, err := selectBrokers(brokerDB, args, region, category)
	if err != nil {
		return err
	}
	if len(brokers) == 0 {
		fmt.Println("No brokers match.")
		return nil
	}

	tmpl := cfg.Options.Template
	if tmpl == "" {
		tmpl = "gdpr"
	}

	if dryRun {
		fmt.Printf("🔍 Would record %d broker(s) as sent manually (template %q):\n", len(brokers), tmpl)
		for _, b := range brokers {
			fmt.Printf("   - %s (%s)\n", b.Name, b.ID)
		}
		return nil
	}

	store, err := history.NewStore(history.DBPathFor(resolveConfigPath()))
	if err != nil {
		return fmt.Errorf("failed to open history: %w", err)
	}
	defer func() { _ = store.Close() }()

	recorded := 0
	for _, b := range brokers {
		if err := store.Add(manualSentRecord(activeProfile.ID, b, tmpl)); err != nil {
			return fmt.Errorf("failed to record %s: %w", b.ID, err)
		}
		recorded++
	}

	fmt.Println("✅ Recorded as sent (manually)")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	if len(cfg.GetProfiles()) > 1 {
		fmt.Printf("   Profile: %s\n", activeProfile.ID)
	}
	fmt.Printf("   %d broker(s). See `eraser status` or `eraser export`.\n", recorded)
	return nil
}
