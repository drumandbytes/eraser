package main

import (
	"context"
	"fmt"
	"time"

	"github.com/eraser-privacy/eraser/internal/broker"
	"github.com/eraser-privacy/eraser/internal/config"
	"github.com/eraser-privacy/eraser/internal/history"
	"github.com/eraser-privacy/eraser/internal/inbox"
	"github.com/spf13/cobra"
)

func cleanupBouncesCmd() *cobra.Command {
	var (
		remove bool
		days   int
	)

	cmd := &cobra.Command{
		Use:   "cleanup-bounces",
		Short: "Find and remove bounced broker email addresses",
		Long: `Scan your inbox for bounced/undeliverable emails and identify
invalid broker email addresses. Optionally remove them from the database.

By default, this command shows what would be removed without making changes.
Use --remove to actually remove the invalid brokers from the database.

Examples:
  eraser cleanup-bounces                 # Show bounced emails (dry run)
  eraser cleanup-bounces --remove        # Remove bounced brokers
  eraser cleanup-bounces --days 30       # Look back 30 days`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCleanupBounces(remove, days)
		},
	}

	cmd.Flags().BoolVar(&remove, "remove", false, "Actually remove bounced brokers from database")
	cmd.Flags().IntVar(&days, "days", 30, "Number of days to scan for bounced emails")

	return cmd
}

func runCleanupBounces(remove bool, days int) error {
	// Load config
	cfg, err := config.Load(resolveConfigPath())
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Check if inbox is configured
	if !cfg.Inbox.Enabled {
		return fmt.Errorf("inbox monitoring not configured. Run 'eraser init' to set up")
	}

	// Load broker database
	brokerPath := resolveBrokerPath()
	brokerDB, err := broker.LoadFromFile(brokerPath)
	if err != nil {
		return fmt.Errorf("failed to load brokers: %w", err)
	}

	fmt.Println("🔍 Scanning inbox for bounced emails...")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println()

	// Create inbox monitor
	monitor := inbox.NewMonitor(cfg.Inbox, brokerDB.Brokers)

	// Connect
	ctx := context.Background()
	if err := monitor.Connect(ctx); err != nil {
		return fmt.Errorf("failed to connect to inbox: %w", err)
	}
	defer func() { _ = monitor.Disconnect() }()

	// Fetch bounce emails
	bounceEmails, err := monitor.FetchBounceEmails(ctx, days)
	if err != nil {
		return fmt.Errorf("failed to fetch bounce emails: %w", err)
	}

	if len(bounceEmails) == 0 {
		fmt.Println("✓ No bounced emails found!")
		return nil
	}

	fmt.Printf("Found %d bounced email(s):\n\n", len(bounceEmails))

	// Track brokers to remove
	type bouncedBroker struct {
		email      string
		broker     *broker.Broker
		subject    string
		receivedAt time.Time
	}
	var bouncedBrokers []bouncedBroker

	for _, email := range bounceEmails {
		// Extract the bounced recipient
		bouncedRecipient := inbox.ExtractBouncedRecipient(&email)
		if bouncedRecipient == "" {
			fmt.Printf("⚠️  Could not extract bounced address from: %s\n", email.Subject)
			continue
		}

		// Find the broker
		b := brokerDB.FindByEmail(bouncedRecipient)
		if b == nil {
			fmt.Printf("⚠️  %s - not found in broker database\n", bouncedRecipient)
			continue
		}

		fmt.Printf("❌ %s\n", bouncedRecipient)
		fmt.Printf("   Broker: %s (%s)\n", b.Name, b.ID)
		fmt.Printf("   Subject: %s\n", truncateString(email.Subject, 60))
		fmt.Printf("   Date: %s\n", email.ReceivedAt.Format("2006-01-02"))
		fmt.Println()

		bouncedBrokers = append(bouncedBrokers, bouncedBroker{
			email:      bouncedRecipient,
			broker:     b,
			subject:    email.Subject,
			receivedAt: email.ReceivedAt,
		})
	}

	if len(bouncedBrokers) == 0 {
		fmt.Println("✓ No broker email addresses need to be removed")
		return nil
	}

	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	if !remove {
		fmt.Printf("\n📊 Found %d broker(s) with invalid email addresses\n", len(bouncedBrokers))
		fmt.Println("Run with --remove to delete these brokers from the database")
		return nil
	}

	// Remove the brokers
	fmt.Printf("\n🗑️  Removing %d broker(s) from database...\n\n", len(bouncedBrokers))

	removed := 0
	for _, bb := range bouncedBrokers {
		if brokerDB.RemoveByEmail(bb.email) != nil {
			fmt.Printf("✓ Removed %s (%s)\n", bb.broker.Name, bb.email)
			removed++
		}
	}

	// Save with backup
	if err := brokerDB.SaveWithBackup(brokerPath); err != nil {
		return fmt.Errorf("failed to save broker database: %w", err)
	}

	fmt.Println()
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Printf("✓ Removed %d broker(s) with invalid email addresses\n", removed)
	fmt.Printf("  Backup saved to: %s.bak\n", brokerPath)

	return nil
}

func markBouncedCmd() *cobra.Command {
	var note string

	cmd := &cobra.Command{
		Use:   "mark-bounced <broker-id> [<broker-id>...]",
		Short: "Correct the record for broker(s) whose email actually bounced",
		Long: `'send' records a broker as sent the moment your SMTP provider accepts
the message for delivery - that's the only signal a normal send gets. A
bounce is a separate email that arrives later (sometimes minutes, sometimes
longer), and without inbox monitoring configured, nothing links it back to
that history record automatically.

If you've spotted a bounce yourself - by reading your inbox, or now that a
Gmail connector lets an assistant read it for you - use this to correct the
record once you're done acting on it (e.g. after fixing the broker's contact
info in data/brokers.yaml). It flips that broker's most recent "sent" record
to "failed", which:

  - removes it from the 25-day resend cooldown, so the next 'eraser send'
    retries it automatically (no need for --resend)
  - makes 'eraser status' reflect what actually happened, instead of
    claiming a delivery that didn't succeed

This only corrects history - it doesn't resend anything by itself.

Example:
  eraser mark-bounced crawlbee ivy-tech-re jverify --note "contact fixed 2026-08-20"`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMarkBounced(args, note)
		},
	}

	cmd.Flags().StringVar(&note, "note", "", "Optional note recorded with the correction (why/when it bounced)")

	return cmd
}

func runMarkBounced(brokerIDs []string, note string) error {
	cfg, err := config.Load(resolveConfigPath())
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	activeProfile, err := resolveProfile(cfg)
	if err != nil {
		return err
	}

	store, err := history.NewStore(history.DBPathFor(resolveConfigPath()))
	if err != nil {
		return fmt.Errorf("failed to initialize history: %w", err)
	}
	defer func() { _ = store.Close() }()

	errMsg := "bounced - manually confirmed"
	if note != "" {
		errMsg = errMsg + ": " + note
	}

	updated, skipped := 0, 0
	for _, id := range brokerIDs {
		n, err := store.MarkFailed(activeProfile.ID, id, errMsg)
		if err != nil {
			return fmt.Errorf("failed to mark %s: %w", id, err)
		}
		if n == 0 {
			fmt.Printf("⚠️  %s - no \"sent\" record found to correct (never sent, or already marked failed)\n", id)
			skipped++
			continue
		}
		fmt.Printf("✓ %s - marked failed, will be retried on next 'eraser send'\n", id)
		updated++
	}

	fmt.Println()
	if skipped > 0 {
		fmt.Printf("Updated %d broker(s), %d skipped.\n", updated, skipped)
	} else {
		fmt.Printf("Updated %d broker(s).\n", updated)
	}
	return nil
}

// truncateString truncates a string to the specified length
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
