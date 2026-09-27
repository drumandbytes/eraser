package main

import (
	"context"
	"fmt"
	"os/signal"
	"sync"
	"syscall"

	"github.com/drumandbytes/eraser/internal/broker"
	"github.com/drumandbytes/eraser/internal/config"
	"github.com/drumandbytes/eraser/internal/history"
	"github.com/drumandbytes/eraser/internal/inbox"
	"github.com/spf13/cobra"
)

func monitorCmd() *cobra.Command {
	var days int
	var once bool
	var watch bool

	cmd := &cobra.Command{
		Use:   "monitor",
		Short: "Monitor inbox for broker responses",
		Long: `Connect to your email inbox via IMAP and monitor for responses from data brokers.

This command will:
- Fetch recent emails from known broker domains
- Classify responses (form required, confirmation needed, success, etc.)
- Extract form URLs and confirmation links
- Store results for the pipeline to process

Requires inbox configuration in config.yaml with IMAP settings.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMonitor(days, once, watch)
		},
	}

	cmd.Flags().IntVar(&days, "days", 7, "Number of days to look back for emails")
	cmd.Flags().BoolVar(&once, "once", false, "Check inbox once and exit (don't watch for new emails)")
	cmd.Flags().BoolVar(&watch, "watch", false, "Continuously watch for new emails")

	return cmd
}

func runMonitor(days int, once bool, watch bool) error {
	cfg, err := config.Load(resolveConfigPath())
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	inboxes := cfg.ConfiguredInboxes()
	if len(inboxes) == 0 {
		fmt.Println("📧 Inbox monitoring is not configured.")
		fmt.Println()
		fmt.Println("To enable inbox monitoring, add the following to your config.yaml:")
		fmt.Println()
		fmt.Println("inbox:")
		fmt.Println("  enabled: true")
		fmt.Println("  provider: gmail")
		fmt.Println("  email: your-email@gmail.com")
		fmt.Println("  password: your-app-password  # Use an App Password, not your main password")
		fmt.Println()
		fmt.Println("For Gmail, you'll need to:")
		fmt.Println("  1. Enable 2-Step Verification")
		fmt.Println("  2. Generate an App Password at https://myaccount.google.com/apppasswords")
		fmt.Println("  3. Enable IMAP in Gmail settings")
		return fmt.Errorf("inbox: monitoring is not enabled in config")
	}

	brokerDB, err := broker.Load(brokerFile)
	if err != nil {
		return fmt.Errorf("failed to load brokers: %w", err)
	}

	store, err := history.NewStore(history.DBPathFor(resolveConfigPath()))
	if err != nil {
		return fmt.Errorf("failed to initialize history: %w", err)
	}
	defer func() { _ = store.Close() }()

	// NotifyContext (not a bare signal.Notify + goroutine) so the handler is
	// released on return - `eraser auto` calls this once per cycle.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if len(inboxes) > 1 {
		fmt.Printf("📬 Monitoring %d configured inboxes for broker responses (last %d days)...\n", len(inboxes), days)
	}

	if !watch {
		// Plain scans run sequentially - simpler, and avoids concurrent
		// writes to the shared SQLite history store (see the --watch note
		// below).
		for _, inboxCfg := range inboxes {
			if err := scanInbox(ctx, inboxCfg, brokerDB, store, days, once, watch); err != nil {
				return err
			}
		}
		return nil
	}

	// --watch blocks per inbox, so several inboxes need goroutines. NewStore's
	// WAL + busy_timeout makes their concurrent writes safe.
	var wg sync.WaitGroup
	errs := make([]error, len(inboxes))
	for i, inboxCfg := range inboxes {
		wg.Add(1)
		go func(i int, inboxCfg config.InboxConfig) {
			defer wg.Done()
			errs[i] = scanInbox(ctx, inboxCfg, brokerDB, store, days, once, watch)
		}(i, inboxCfg)
	}
	wg.Wait()

	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

// scanInbox classifies and stores one inbox's broker replies, and with --watch
// keeps watching until ctx ends.
func scanInbox(ctx context.Context, inboxCfg config.InboxConfig, brokerDB *broker.BrokerDatabase, store *history.Store, days int, once bool, watch bool) error {
	monitor := inbox.NewMonitor(inboxCfg, brokerDB.Brokers)

	if err := monitor.Connect(ctx); err != nil {
		return fmt.Errorf("failed to connect to inbox %s: %w", inboxCfg.Email, err)
	}
	defer func() { _ = monitor.Disconnect() }()

	fmt.Printf("📬 Monitoring %s for broker responses (last %d days)...\n\n", inboxCfg.Email, days)

	res, err := monitor.ScanAndStore(ctx, store, inbox.ScanOptions{Days: days})
	if err != nil {
		return err
	}
	fmt.Printf("Found %d emails from data brokers in %s, %d new\n\n", res.Summary.Total, inboxCfg.Email, len(res.New))
	for _, r := range res.New {
		printClassifiedResponse(r)
	}
	if res.Archived > 0 {
		fmt.Printf("📁 Archived %d emails to '%s'\n", res.Archived, inboxCfg.ArchiveFolder)
	}

	summary := res.Summary
	fmt.Println()
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Printf("📊 Summary for %s:\n", inboxCfg.Email)
	fmt.Printf("  Total responses:     %d\n", summary.Total)
	fmt.Printf("  ✅ Success:          %d\n", summary.Success)
	fmt.Printf("  📝 Form required:    %d\n", summary.FormRequired)
	fmt.Printf("  🔗 Confirm required: %d\n", summary.ConfirmRequired)
	fmt.Printf("  ❌ Rejected:         %d\n", summary.Rejected)
	fmt.Printf("  ⏳ Pending:          %d\n", summary.Pending)
	fmt.Printf("  ❓ Unknown:          %d\n", summary.Unknown)
	fmt.Printf("  👁️  Need review:      %d\n", summary.NeedReview)

	if once || !watch {
		return nil
	}

	fmt.Println()
	fmt.Printf("👀 Watching %s for new emails... (Ctrl+C to stop)\n", inboxCfg.Email)
	err = monitor.WatchForNewEmails(ctx, func(email inbox.Email) {
		fmt.Println()
		fmt.Printf("📨 New email from %s (%s)\n", email.BrokerName, email.From)
		classified, _, err := inbox.RecordReply(store, &email, false)
		if err != nil {
			fmt.Printf("⚠️  Failed to store response: %v\n", err)
		}
		printClassifiedResponse(classified)
	})
	if err != nil && err != context.Canceled {
		return fmt.Errorf("watch error on %s: %w", inboxCfg.Email, err)
	}
	return nil
}

func printClassifiedResponse(r inbox.ClassifiedResponse) {
	var icon string
	switch r.Type {
	case inbox.ResponseSuccess:
		icon = "✅"
	case inbox.ResponseFormRequired:
		icon = "📝"
	case inbox.ResponseConfirmationRequired:
		icon = "🔗"
	case inbox.ResponseRejected:
		icon = "❌"
	case inbox.ResponsePending:
		icon = "⏳"
	default:
		icon = "❓"
	}

	fmt.Printf("%s %s - %s\n", icon, r.Email.BrokerName, r.Type)
	fmt.Printf("   Subject: %s\n", r.Email.Subject)

	if r.FormURL != "" {
		fmt.Printf("   📝 Form URL: %s\n", r.FormURL)
	}
	if r.ConfirmURL != "" {
		fmt.Printf("   🔗 Confirm URL: %s\n", r.ConfirmURL)
	}
	if r.NeedsReview {
		fmt.Printf("   ⚠️  Confidence: %.0f%% - manual review recommended\n", r.Confidence*100)
	}
}
