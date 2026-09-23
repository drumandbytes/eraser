package main

import (
	"context"
	"fmt"
	"os"
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

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		fmt.Println("\nShutting down...")
		cancel()
	}()

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

	// --watch blocks per inbox (each one waits for new mail indefinitely),
	// so watching more than one inbox genuinely needs concurrency here.
	//
	// ponytail: this writes to the shared history.Store from multiple
	// goroutines with no WAL/busy_timeout tuning, so concurrent inserts can
	// occasionally hit SQLITE_BUSY under real contention (rare: broker
	// replies are infrequent and NewStore's single *sql.DB already
	// serializes at the connection-pool level, but not guaranteed). Add
	// PRAGMA busy_timeout in history.NewStore if this shows up in practice.
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

// scanInbox connects to one IMAP inbox, classifies and stores its broker
// replies, and (with --watch) keeps watching it for new mail until ctx is
// cancelled. Every profile that shares this inbox (or falls back to it) is
// scanned together - a shared inbox carries replies for every such
// profile's sent requests, so each reply is attributed after the fact via
// ResolveProfileForBroker rather than to any one of them.
func scanInbox(ctx context.Context, inboxCfg config.InboxConfig, brokerDB *broker.BrokerDatabase, store *history.Store, days int, once bool, watch bool) error {
	monitor := inbox.NewMonitor(inboxCfg, brokerDB.Brokers)

	if err := monitor.Connect(ctx); err != nil {
		return fmt.Errorf("failed to connect to inbox %s: %w", inboxCfg.Email, err)
	}
	defer func() { _ = monitor.Disconnect() }()

	fmt.Printf("📬 Monitoring %s for broker responses (last %d days)...\n", inboxCfg.Email, days)
	fmt.Println()

	emails, err := monitor.FetchBrokerEmails(ctx, days)
	if err != nil {
		return fmt.Errorf("failed to fetch emails from %s: %w", inboxCfg.Email, err)
	}

	if len(emails) == 0 {
		fmt.Printf("No emails from known brokers found in %s.\n", inboxCfg.Email)
		if !watch {
			return nil
		}
	}

	// Classify and process each email
	fmt.Printf("Found %d emails from data brokers in %s\n", len(emails), inboxCfg.Email)
	fmt.Println()

	var responses []inbox.ClassifiedResponse
	for _, email := range emails {
		classified := inbox.ClassifyResponse(&email)
		responses = append(responses, classified)

		profileID, err := store.ResolveProfileForBroker(email.BrokerID)
		if err != nil {
			profileID = history.DefaultProfileID
		}

		brokerResp := &history.BrokerResponse{
			ProfileID:    profileID,
			BrokerID:     email.BrokerID,
			BrokerName:   email.BrokerName,
			ResponseType: string(classified.Type),
			EmailFrom:    email.From,
			EmailSubject: email.Subject,
			FormURL:      classified.FormURL,
			ConfirmURL:   classified.ConfirmURL,
			Confidence:   classified.Confidence,
			NeedsReview:  classified.NeedsReview,
			ReceivedAt:   email.ReceivedAt,
		}

		if err := store.AddBrokerResponse(brokerResp); err != nil {
			fmt.Printf("⚠️  Failed to store response: %v\n", err)
		}

		// Update pipeline status for the broker
		var pipelineStatus history.PipelineStatus
		switch classified.Type {
		case inbox.ResponseSuccess:
			pipelineStatus = history.PipelineConfirmed
		case inbox.ResponseFormRequired:
			pipelineStatus = history.PipelineFormRequired
		case inbox.ResponseConfirmationRequired:
			pipelineStatus = history.PipelineAwaitingConfirmation
		case inbox.ResponseRejected:
			pipelineStatus = history.PipelineRejected
		case inbox.ResponsePending:
			pipelineStatus = history.PipelineAwaitingResponse
		default:
			pipelineStatus = history.PipelineAwaitingResponse
		}

		// Ignore error if no matching record
		_ = store.UpdatePipelineStatus(profileID, email.BrokerID, pipelineStatus)

		printClassifiedResponse(classified)
	}

	// Archive processed emails if enabled
	if inboxCfg.AutoArchive && len(emails) > 0 {
		archiveFolder := inboxCfg.ArchiveFolder

		// Ensure archive folder exists
		if err := monitor.EnsureFolderExists(archiveFolder); err != nil {
			fmt.Printf("⚠️  Could not create archive folder: %v\n", err)
		} else {
			// Collect UIDs to archive
			var uidsToArchive []uint32
			for _, email := range emails {
				if email.UID > 0 {
					uidsToArchive = append(uidsToArchive, email.UID)
				}
			}

			if len(uidsToArchive) > 0 {
				if err := monitor.ArchiveEmails(uidsToArchive, archiveFolder); err != nil {
					fmt.Printf("⚠️  Could not archive emails: %v\n", err)
				} else {
					fmt.Printf("📁 Archived %d emails to '%s'\n", len(uidsToArchive), archiveFolder)
				}
			}
		}
	}

	summary := inbox.SummarizeResponses(responses)
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

	if once {
		return nil
	}

	if watch {
		fmt.Println()
		fmt.Printf("👀 Watching %s for new emails... (Ctrl+C to stop)\n", inboxCfg.Email)

		err := monitor.WatchForNewEmails(ctx, func(email inbox.Email) {
			fmt.Println()
			fmt.Printf("📨 New email from %s (%s)\n", email.BrokerName, email.From)

			classified := inbox.ClassifyResponse(&email)
			printClassifiedResponse(classified)

			profileID, err := store.ResolveProfileForBroker(email.BrokerID)
			if err != nil {
				profileID = history.DefaultProfileID
			}

			brokerResp := &history.BrokerResponse{
				ProfileID:    profileID,
				BrokerID:     email.BrokerID,
				BrokerName:   email.BrokerName,
				ResponseType: string(classified.Type),
				EmailFrom:    email.From,
				EmailSubject: email.Subject,
				FormURL:      classified.FormURL,
				ConfirmURL:   classified.ConfirmURL,
				Confidence:   classified.Confidence,
				NeedsReview:  classified.NeedsReview,
				ReceivedAt:   email.ReceivedAt,
			}
			if err := store.AddBrokerResponse(brokerResp); err != nil {
				fmt.Printf("⚠️  Failed to store response: %v\n", err)
			}
		})

		if err != nil && err != context.Canceled {
			return fmt.Errorf("watch error on %s: %w", inboxCfg.Email, err)
		}
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
