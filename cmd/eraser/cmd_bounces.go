package main

import (
	"context"
	"fmt"

	"github.com/drumandbytes/eraser/internal/bounces"
	"github.com/drumandbytes/eraser/internal/broker"
	"github.com/drumandbytes/eraser/internal/config"
	"github.com/drumandbytes/eraser/internal/history"
	"github.com/spf13/cobra"
)

func cleanupBouncesCmd() *cobra.Command {
	var (
		remove bool
		days   int
	)

	cmd := &cobra.Command{
		Use:   "cleanup-bounces",
		Short: "Find and clear bounced broker email addresses",
		Long: `Scan your inbox for bounced/undeliverable emails and identify
invalid broker email addresses. Optionally clear them in the database.

A bounce usually means the company changed its privacy-request address, not
that it stopped existing, so this never deletes the broker itself - it only
clears the dead email address (keeping name, category, website, and opt-out
URL) and records the old address in the broker's notes. Use
'list-brokers --missing-email' afterward to find brokers that need a new
address.

By default, this command shows what would be cleared without making changes.
Use --remove to actually clear the invalid addresses in the database.

Examples:
  eraser cleanup-bounces                 # Show bounced emails (dry run)
  eraser cleanup-bounces --remove        # Clear bounced addresses
  eraser cleanup-bounces --days 30       # Look back 30 days`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCleanupBounces(remove, days)
		},
	}

	cmd.Flags().BoolVar(&remove, "remove", false, "Actually clear bounced email addresses in the database")
	cmd.Flags().IntVar(&days, "days", 30, "Number of days to scan for bounced emails")

	return cmd
}

func runCleanupBounces(remove bool, days int) error {
	cfg, err := config.Load(resolveConfigPath())
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	inboxes := cfg.ConfiguredInboxes()
	if len(inboxes) == 0 {
		return fmt.Errorf("inbox monitoring not configured. Run 'eraser init' to set up")
	}

	brokerPath, maintaining := listFileWritePath()
	var brokerDB *broker.BrokerDatabase
	if maintaining {
		brokerDB, err = broker.LoadFromFile(brokerPath)
	} else {
		brokerDB, err = broker.Load("") // corrections go to broker.LocalPath, below
	}
	if err != nil {
		return fmt.Errorf("failed to load brokers: %w", err)
	}

	store, err := openHistoryStore(history.DBPathFor(resolveConfigPath()))
	if err != nil {
		return fmt.Errorf("failed to initialize history: %w", err)
	}
	defer func() { _ = store.Close() }()

	fmt.Println("🔍 Scanning inbox for bounced emails...")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println()

	rep, err := bounces.Scan(context.Background(), inboxes, brokerDB, everSentBrokerIDs(cfg, store), days)
	if err != nil {
		return err
	}
	if rep.Total == 0 {
		fmt.Println("✓ No bounced emails found!")
		return nil
	}

	fmt.Printf("Found %d bounced email(s):\n\n", rep.Total)
	for _, it := range rep.Items {
		switch it.Kind {
		case bounces.NoRecipient:
			fmt.Printf("⚠️  Could not extract bounced address from: %s\n", it.Subject)
		case bounces.NotInList:
			fmt.Printf("⚠️  %s - not found in broker database\n", it.Email)
		case bounces.NeverSent:
			fmt.Printf("⚠️  %s - looks like a bounce for %s (%s), but there's no record of ever emailing them - skipping as a likely misclassified or spoofed message\n", it.Email, it.Broker.Name, it.Broker.ID)
		case bounces.Dead:
			fmt.Printf("❌ %s\n", it.Email)
			fmt.Printf("   Broker: %s (%s)\n", it.Broker.Name, it.Broker.ID)
			fmt.Printf("   Subject: %s\n", truncateString(it.Subject, 60))
			fmt.Printf("   Date: %s\n", it.ReceivedAt.Format("2006-01-02"))
			fmt.Println()
		}
	}

	dead := rep.Dead()
	if len(dead) == 0 {
		fmt.Println("✓ No broker email addresses need to be removed")
		return nil
	}

	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	if !remove {
		fmt.Printf("\n📊 Found %d broker(s) with invalid email addresses\n", len(dead))
		fmt.Println("Run with --remove to clear these addresses in the database")
		return nil
	}

	// Clear the dead addresses (the broker entries themselves are kept)
	fmt.Printf("\n🧹 Clearing %d broker email address(es)...\n\n", len(dead))
	listPath := ""
	if maintaining {
		listPath = brokerPath
	}
	cleared, err := bounces.Clear(listPath, dead)
	for _, it := range cleared {
		fmt.Printf("✓ Cleared %s (%s)\n", it.Broker.Name, it.Email)
	}
	if err != nil {
		return err
	}

	fmt.Println()
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Printf("✓ Cleared %d broker email address(es) - entries kept, ready for a new address\n", len(cleared))
	fmt.Println("  Run 'eraser list-brokers --missing-email' to see them")
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
mail connector lets an assistant read it for you - use this to correct the
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

	store, err := openHistoryStore(history.DBPathFor(resolveConfigPath()))
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

// everSentBrokerIDs returns brokers any profile has ever sent to.
//
// Bounces are classified from unauthenticated From/Subject text and every
// broker address is public, so anyone could forge an NDR and have --remove
// clear a real contact. Limiting to brokers we've actually emailed shrinks
// that to the user's own contacts.
func everSentBrokerIDs(cfg *config.Config, store *history.Store) map[string]bool {
	return bounces.EverSent(cfg, store)
}

// truncateString truncates a string to the specified length
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
