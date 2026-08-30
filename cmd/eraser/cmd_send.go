package main

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/eraser-privacy/eraser/internal/broker"
	"github.com/eraser-privacy/eraser/internal/config"
	"github.com/eraser-privacy/eraser/internal/email"
	"github.com/eraser-privacy/eraser/internal/history"
	"github.com/eraser-privacy/eraser/internal/template"
	"github.com/spf13/cobra"
)

var (
	dryRun           bool
	ignoreDailyLimit bool
	resend           bool
	templateOverride string
)

// resendCooldown is how long after a successful send a broker is skipped by
// default on subsequent `send` runs - long enough that resuming an
// in-progress backlog (spread across the daily cap) doesn't re-email
// brokers from yesterday, short enough that the monthly re-run still hits
// everyone again once brokers have had time to re-list you.
const resendCooldown = 25 * 24 * time.Hour

func sendCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "send",
		Short: "Send removal requests to data brokers",
		Long: `Send data removal requests to all configured data brokers.

To avoid tripping your email provider's daily sending limit (or looking like
bulk spam to it), each run only sends up to options.daily_send_limit emails
(default 450) and skips brokers it already emailed successfully in the last
25 days. Run it again - the same day or tomorrow - to keep working through
a large broker list; already-sent brokers are automatically skipped, so it's
safe to just re-run 'eraser send' until it reports nothing left to do.

The cooldown is tracked per request type, so a subject access request and an
erasure request are separate sends to the same broker. UK residents can run:

  eraser send --template uk-access     # ask what they hold (Art. 15)
  eraser send --template uk-erasure    # ask them to delete it (Art. 17)
  eraser send --template uk-combined   # both in one email, access answered first

Running the access pass first is usually worth it: the reply names the source
they bought your data from and the recipients they sold it to, which is how
you find the next brokers to chase. Once a broker has erased your record it
can no longer tell you any of that.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSend()
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview emails without sending")
	cmd.Flags().BoolVar(&ignoreDailyLimit, "ignore-daily-limit", false, "Send to all matching brokers in one run, ignoring the daily cap (only if your provider can handle the volume)")
	cmd.Flags().BoolVar(&resend, "resend", false, "Also re-send to brokers already emailed within the last 25 days")
	cmd.Flags().StringVar(&templateOverride, "template", "", "Template for this run, overriding options.template (one of: "+strings.Join(template.TemplateNames(), ", ")+")")

	return cmd
}

func runSend() error {
	cfg, err := config.Load(resolveConfigPath())
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}

	activeProfile, err := resolveProfile(cfg)
	if err != nil {
		return err
	}

	// Override dry-run from command line
	if dryRun {
		cfg.Options.DryRun = true
	}

	brokerDB, err := broker.LoadFromFile(resolveBrokerPath())
	if err != nil {
		return fmt.Errorf("failed to load brokers: %w", err)
	}

	// Filter brokers
	brokers := brokerDB.Filter(cfg.Options.Regions, cfg.Options.ExcludedBrokers, cfg.Options.ExcludedCategories)
	if len(brokers) == 0 {
		fmt.Println("No brokers to process.")
		return nil
	}

	// Resolve which template this run uses before anything reads it: --template
	// wins over options.template, and the request type it implies drives both
	// the cooldown lookup below and what gets recorded against each send.
	activeTemplate := cfg.Options.Template
	if templateOverride != "" {
		if !slices.Contains(template.TemplateNames(), templateOverride) {
			return fmt.Errorf("unknown template %q (available: %s)",
				templateOverride, strings.Join(template.TemplateNames(), ", "))
		}
		activeTemplate = templateOverride
	}
	requestType := template.RequestTypeFor(activeTemplate)

	// Initialize history store early - needed for both the resend-cooldown
	// skip and the daily send cap below.
	store, err := history.NewStore(history.DBPathFor(resolveConfigPath()))
	if err != nil {
		return fmt.Errorf("failed to initialize history: %w", err)
	}
	defer func() { _ = store.Close() }()

	// Skip brokers already successfully emailed within the cooldown window,
	// unless --resend was passed. This is what makes it safe to just re-run
	// `eraser send` to resume a large backlog without double-emailing
	// brokers from an earlier run this same campaign.
	if !resend && !cfg.Options.DryRun {
		lastSent, err := store.LastSuccessfulSendTimes(activeProfile.ID)
		if err != nil {
			return fmt.Errorf("failed to check send history: %w", err)
		}

		filtered := brokers[:0:0]
		skipped := 0
		for _, b := range brokers {
			// Keyed on request type too, so having already sent an erasure
			// request doesn't suppress a subject access request to the same
			// broker (and vice versa) - they exercise different rights.
			key := history.SendKey{BrokerID: b.ID, RequestType: requestType}
			if sentAt, ok := lastSent[key]; ok && time.Since(sentAt) < resendCooldown {
				skipped++
				continue
			}
			filtered = append(filtered, b)
		}
		if skipped > 0 {
			fmt.Printf("⏭️  Skipping %d broker(s) already sent a %s request in the last %d days (use --resend to override)\n", skipped, requestType, int(resendCooldown.Hours()/24))
		}
		brokers = filtered
	}

	if len(brokers) == 0 {
		fmt.Printf("Nothing to send - every broker has had a %s request recently. Run with --resend to force, or check back after the cooldown window.\n", requestType)
		return nil
	}

	// Enforce the rolling daily send cap so a large broker list can't blow
	// past the provider's per-day sending limit or read as bulk-spam
	// behavior. Skipped entirely for --dry-run and --ignore-daily-limit.
	if !cfg.Options.DryRun && !ignoreDailyLimit {
		sentLast24h, err := store.CountSentSince(activeProfile.ID, time.Now().Add(-24*time.Hour))
		if err != nil {
			return fmt.Errorf("failed to check daily send count: %w", err)
		}
		budget := cfg.Options.DailySendLimit - sentLast24h
		if budget <= 0 {
			fmt.Printf("📅 Daily send limit reached (%d/%d sent in the last 24h). Run again later, or use --ignore-daily-limit to override.\n",
				sentLast24h, cfg.Options.DailySendLimit)
			return nil
		}
		if budget < len(brokers) {
			fmt.Printf("📅 Daily send limit: sending %d of %d remaining brokers (%d already sent in the last 24h, cap is %d). Re-run later or tomorrow for the rest.\n",
				budget, len(brokers), sentLast24h, cfg.Options.DailySendLimit)
			brokers = brokers[:budget]
		}
	}

	// Initialize template engine
	tmplEngine, err := template.NewEngine()
	if err != nil {
		return fmt.Errorf("failed to initialize templates: %w", err)
	}

	// Initialize email sender (unless dry-run)
	var sender email.Sender
	if !cfg.Options.DryRun {
		sender, err = email.NewSender(cfg.Email)
		if err != nil {
			return fmt.Errorf("failed to initialize email sender: %w", err)
		}
	}

	// Process brokers
	if cfg.Options.DryRun {
		fmt.Println("🔍 DRY RUN MODE - No emails will be sent")
		fmt.Println()
	}

	if len(cfg.GetProfiles()) > 1 {
		fmt.Printf("👤 Profile: %s (%s)\n", activeProfile.ID, activeProfile.FullName())
	}
	fmt.Printf("📤 Processing %d brokers...\n", len(brokers))
	fmt.Println()

	successCount := 0
	failCount := 0

	for i, b := range brokers {
		fmt.Printf("[%d/%d] %s (%s)\n", i+1, len(brokers), b.Name, b.Email)

		// Brokers with no email on file (confirmed defunct, or "use the web
		// form instead" cases documented in their notes) have nothing to
		// send to - skip rather than let the SMTP layer choke on an empty
		// recipient and burn a daily-cap slot on a guaranteed failure.
		if strings.TrimSpace(b.Email) == "" {
			if b.OptOutURL != "" {
				fmt.Printf("  ⏭️  No email on file - use the opt-out form instead: %s\n", b.OptOutURL)
			} else {
				fmt.Printf("  ⏭️  No email on file - see notes in brokers.yaml\n")
			}
			continue
		}

		// Render email
		emailMsg, err := tmplEngine.Render(activeTemplate, activeProfile.Profile, b)
		if err != nil {
			fmt.Printf("  ❌ Failed to render template: %v\n", err)
			failCount++
			continue
		}

		if cfg.Options.DryRun {
			fmt.Printf("  📧 Would send: %s\n", emailMsg.Subject)
			fmt.Printf("  📍 To: %s\n", b.Email)
			successCount++
		} else {
			// Send email
			msg := email.Message{
				To:      b.Email,
				From:    cfg.Email.From,
				Subject: emailMsg.Subject,
				Body:    emailMsg.Body,
			}

			ctx := context.WithValue(context.Background(), email.SequenceKey, i)
			result := sender.Send(ctx, msg)

			// Record in history
			record := &history.Record{
				ProfileID:   activeProfile.ID,
				BrokerID:    b.ID,
				BrokerName:  b.Name,
				Email:       b.Email,
				Template:    activeTemplate,
				RequestType: requestType,
				SentAt:      time.Now(),
			}

			if result.Success {
				record.Status = history.StatusSent
				record.MessageID = result.MessageID
				fmt.Printf("  ✅ Sent successfully\n")
				successCount++
			} else {
				record.Status = history.StatusFailed
				record.Error = result.Error.Error()
				fmt.Printf("  ❌ Failed: %v\n", result.Error)
				failCount++
			}

			if err := store.Add(record); err != nil {
				fmt.Printf("  ⚠️  Failed to record history: %v\n", err)
			}

			// Rate limiting
			if i < len(brokers)-1 {
				time.Sleep(time.Duration(cfg.Options.RateLimitMs) * time.Millisecond)
			}
		}
	}

	fmt.Println()
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	if cfg.Options.DryRun {
		fmt.Printf("📊 Dry run complete: %d brokers would receive emails\n", successCount)
	} else {
		fmt.Printf("📊 Complete: %d sent, %d failed\n", successCount, failCount)
	}

	return nil
}
