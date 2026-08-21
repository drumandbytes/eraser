package main

import (
	"fmt"

	"github.com/eraser-privacy/eraser/internal/config"
	"github.com/eraser-privacy/eraser/internal/history"
	"github.com/spf13/cobra"
)

func statusCmd() *cobra.Command {
	var limit int

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show removal request history and statistics",
		Long:  "Display recent removal requests and overall statistics.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStatus(limit)
		},
	}

	cmd.Flags().IntVar(&limit, "limit", 20, "Number of recent requests to show")

	return cmd
}

func runStatus(limit int) error {
	cfg, err := config.Load(resolveConfigPath())
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	activeProfile, err := resolveProfile(cfg)
	if err != nil {
		return err
	}

	store, err := history.NewStore(history.DefaultDBPath())
	if err != nil {
		return fmt.Errorf("failed to open history: %w", err)
	}
	defer func() { _ = store.Close() }()

	// Get overall stats
	total, sent, failed, err := store.GetStats(activeProfile.ID)
	if err != nil {
		return fmt.Errorf("failed to get stats: %w", err)
	}

	// Get monthly stats
	monthlySent, monthlyFailed, err := store.GetMonthlyStats(activeProfile.ID)
	if err != nil {
		return fmt.Errorf("failed to get monthly stats: %w", err)
	}

	fmt.Println("📊 Eraser Statistics")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	if len(cfg.GetProfiles()) > 1 {
		fmt.Printf("👤 Profile: %s\n", activeProfile.ID)
	}
	fmt.Println()
	fmt.Println("All Time:")
	fmt.Printf("  Total requests: %d\n", total)
	fmt.Printf("  Sent: %d\n", sent)
	fmt.Printf("  Failed: %d\n", failed)
	fmt.Println()
	fmt.Println("This Month:")
	fmt.Printf("  Sent: %d\n", monthlySent)
	fmt.Printf("  Failed: %d\n", monthlyFailed)

	// Get recent requests
	records, err := store.GetRecentRequests(activeProfile.ID, limit)
	if err != nil {
		return fmt.Errorf("failed to get recent requests: %w", err)
	}

	if len(records) > 0 {
		fmt.Println()
		fmt.Printf("📜 Recent Requests (last %d)\n", limit)
		fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

		for _, r := range records {
			status := "✅"
			if r.Status == history.StatusFailed {
				status = "❌"
			}
			fmt.Printf("%s %s - %s (%s)\n",
				status,
				r.SentAt.Format("2006-01-02 15:04"),
				r.BrokerName,
				r.Template,
			)
			if r.Error != "" {
				fmt.Printf("   Error: %s\n", r.Error)
			}
		}
	}

	return nil
}
