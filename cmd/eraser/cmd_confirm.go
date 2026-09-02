package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/drumandbytes/eraser/internal/broker"
	"github.com/drumandbytes/eraser/internal/browser"
	"github.com/drumandbytes/eraser/internal/config"
	"github.com/drumandbytes/eraser/internal/history"
	"github.com/spf13/cobra"
)

func confirmCmd() *cobra.Command {
	var confirmURL string
	var brokerID string
	var pending bool
	var validateDomain bool
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "confirm",
		Short: "Click confirmation links from broker emails",
		Long: `Automatically click confirmation links received from data brokers.

This command makes HTTP GET requests to confirmation URLs to complete the opt-out process.
It follows redirects and verifies success based on the response content.

Examples:
  # Confirm a specific URL
  eraser confirm --url "https://broker.com/confirm?token=abc123"

  # Confirm for a specific broker (using URL from pipeline)
  eraser confirm --broker spokeo

  # Confirm all pending confirmation links
  eraser confirm --pending

  # Preview without actually clicking (dry run)
  eraser confirm --pending --dry-run

Safety features:
  - Domain validation ensures links are from known broker domains
  - Follows redirects up to 10 hops
  - Detects success/failure from response content`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runConfirm(confirmURL, brokerID, pending, validateDomain, dryRun)
		},
	}

	cmd.Flags().StringVar(&confirmURL, "url", "", "Direct confirmation URL to click")
	cmd.Flags().StringVar(&brokerID, "broker", "", "Broker ID to confirm for (uses URL from pipeline)")
	cmd.Flags().BoolVar(&pending, "pending", false, "Confirm all pending confirmation links")
	cmd.Flags().BoolVar(&validateDomain, "validate-domain", true, "Validate URL domain against known brokers (private/localhost addresses are always blocked regardless of this flag)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview links without clicking them")

	return cmd
}

func runConfirm(confirmURL, brokerID string, pending, validateDomain, dryRun bool) error {
	cfg, err := config.Load(resolveConfigPath())
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	activeProfile, err := resolveProfile(cfg)
	if err != nil {
		return err
	}

	// Load brokers for domain validation
	brokerDB, err := broker.Load(brokerFile)
	if err != nil {
		return fmt.Errorf("failed to load brokers: %w", err)
	}

	// Initialize history store
	store, err := history.NewStore(history.DBPathFor(resolveConfigPath()))
	if err != nil {
		return fmt.Errorf("failed to initialize history: %w", err)
	}
	defer func() { _ = store.Close() }()

	// Build list of broker domains for validation
	var brokerDomains []string
	for _, b := range brokerDB.Brokers {
		if b.Website != "" {
			// Extract domain from website
			domain := strings.TrimPrefix(b.Website, "https://")
			domain = strings.TrimPrefix(domain, "http://")
			domain = strings.TrimSuffix(domain, "/")
			if idx := strings.Index(domain, "/"); idx != -1 {
				domain = domain[:idx]
			}
			brokerDomains = append(brokerDomains, domain)

			// Also add the bare domain without www prefix
			if strings.HasPrefix(domain, "www.") {
				brokerDomains = append(brokerDomains, strings.TrimPrefix(domain, "www."))
			}
		}
	}

	// Create confirmation handler
	handler := browser.NewConfirmationHandler(brokerDomains)

	fmt.Println("🔗 Confirmation Link Handler")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println()

	// Determine what to confirm
	var linksToConfirm []struct {
		BrokerID string
		URL      string
	}

	if confirmURL != "" {
		// Direct URL provided
		linksToConfirm = append(linksToConfirm, struct {
			BrokerID string
			URL      string
		}{BrokerID: brokerID, URL: confirmURL})
	} else if brokerID != "" {
		// Get URL for specific broker from pipeline
		responses, err := store.GetBrokerResponses(activeProfile.ID, "confirmation_required", false, 100)
		if err != nil {
			return fmt.Errorf("failed to get broker responses: %w", err)
		}

		found := false
		for _, resp := range responses {
			if resp.BrokerID == brokerID && resp.ConfirmURL != "" {
				linksToConfirm = append(linksToConfirm, struct {
					BrokerID string
					URL      string
				}{BrokerID: resp.BrokerID, URL: resp.ConfirmURL})
				found = true
				break
			}
		}

		if !found {
			return fmt.Errorf("no confirmation URL found for broker: %s", brokerID)
		}
	} else if pending {
		// Get all pending confirmation links
		responses, err := store.GetBrokerResponses(activeProfile.ID, "confirmation_required", false, 100)
		if err != nil {
			return fmt.Errorf("failed to get broker responses: %w", err)
		}

		for _, resp := range responses {
			if resp.ConfirmURL != "" {
				linksToConfirm = append(linksToConfirm, struct {
					BrokerID string
					URL      string
				}{BrokerID: resp.BrokerID, URL: resp.ConfirmURL})
			}
		}

		if len(linksToConfirm) == 0 {
			fmt.Println("✅ No pending confirmation links")
			return nil
		}
	} else {
		return fmt.Errorf("please specify --url, --broker, or --pending")
	}

	fmt.Printf("📋 Confirmation links to process: %d\n", len(linksToConfirm))
	if dryRun {
		fmt.Println("🔍 DRY RUN MODE - Links will not be clicked")
	}
	fmt.Println()

	// Process each link
	successCount := 0
	failCount := 0

	for i, link := range linksToConfirm {
		fmt.Printf("[%d/%d] Processing confirmation link\n", i+1, len(linksToConfirm))
		if link.BrokerID != "" {
			fmt.Printf("       Broker: %s\n", link.BrokerID)
		}
		fmt.Printf("       URL: %s\n", truncateURL(link.URL, 60))

		// Validate domain if requested
		if validateDomain {
			valid, domain, err := handler.ValidateDomain(link.URL)
			if err != nil {
				fmt.Printf("       ❌ Invalid URL: %v\n", err)
				failCount++
				continue
			}
			if !valid {
				fmt.Printf("       ⚠️  Domain %s is not a known broker domain\n", domain)
				fmt.Printf("       Use --validate-domain=false to override\n")
				failCount++
				continue
			}
			fmt.Printf("       ✓ Domain validated: %s\n", domain)
		}

		if dryRun {
			fmt.Printf("       📋 Would click this link (dry run)\n")
			successCount++
			fmt.Println()
			continue
		}

		// Click the confirmation link
		result, err := handler.ClickConfirmationLink(link.URL, false) // Domain already validated above
		if err != nil {
			fmt.Printf("       ❌ Error: %v\n", err)
			failCount++
			continue
		}

		// Show result
		fmt.Printf("       HTTP Status: %d\n", result.StatusCode)
		if len(result.RedirectPath) > 1 {
			fmt.Printf("       Redirects: %d hops\n", len(result.RedirectPath)-1)
		}
		if result.FinalURL != link.URL {
			fmt.Printf("       Final URL: %s\n", truncateURL(result.FinalURL, 60))
		}

		// Extract and show status
		status := handler.ExtractConfirmationStatus(result)
		if result.Success {
			fmt.Printf("       ✅ %s\n", status)
			successCount++

			// Update pipeline status
			if link.BrokerID != "" {
				_ = store.UpdatePipelineStatus(activeProfile.ID, link.BrokerID, history.PipelineConfirmed)
			}
		} else {
			fmt.Printf("       ⚠️  %s\n", status)
			failCount++

			// Still update status to indicate we tried
			if link.BrokerID != "" {
				_ = store.UpdatePipelineStatus(activeProfile.ID, link.BrokerID, history.PipelineFailed)
			}
		}

		fmt.Println()

		// Small delay between confirmations
		if i < len(linksToConfirm)-1 {
			time.Sleep(1 * time.Second)
		}
	}

	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	if dryRun {
		fmt.Printf("📊 Dry run complete: %d links would be clicked\n", len(linksToConfirm))
	} else {
		fmt.Printf("📊 Complete: %d confirmed, %d failed\n", successCount, failCount)
	}

	return nil
}

// truncateURL truncates a URL to the specified length for display
func truncateURL(url string, maxLen int) string {
	if len(url) <= maxLen {
		return url
	}
	return url[:maxLen-3] + "..."
}
