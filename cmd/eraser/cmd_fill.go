package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/eraser-privacy/eraser/internal/broker"
	"github.com/eraser-privacy/eraser/internal/browser"
	"github.com/eraser-privacy/eraser/internal/config"
	"github.com/eraser-privacy/eraser/internal/history"
	"github.com/spf13/cobra"
)

func fillCmd() *cobra.Command {
	var brokerID string
	var formURL string
	var headless bool
	var autoSubmit bool
	var screenshotDir string
	var pending bool
	var waitForCaptcha bool

	cmd := &cobra.Command{
		Use:   "fill",
		Short: "Fill opt-out forms using browser automation",
		Long: `Navigate to data broker opt-out forms and automatically fill them using your profile data.

This command uses headless Chrome to:
- Navigate to opt-out form URLs
- Detect and fill form fields with your personal information
- Detect CAPTCHAs (creates tasks for manual solving)
- Optionally submit the form

Examples:
  # Fill a specific form URL
  eraser fill --url "https://example.com/optout"

  # Fill form for a specific broker (using URL from pipeline)
  eraser fill --broker spokeo

  # Fill all pending forms from the pipeline
  eraser fill --pending

  # Fill with visible browser window (for debugging)
  eraser fill --url "https://example.com/optout" --headless=false

  # Fill form and wait for you to solve CAPTCHA, then auto-submit
  eraser fill --url "https://example.com/optout" --headless=false --wait --submit`,
		RunE: func(cmd *cobra.Command, args []string) error {
			headlessFlagSet := cmd.Flags().Changed("headless")
			return runFill(brokerID, formURL, headless, headlessFlagSet, autoSubmit, screenshotDir, pending, waitForCaptcha)
		},
	}

	cmd.Flags().StringVar(&brokerID, "broker", "", "Broker ID to fill form for (uses URL from pipeline)")
	cmd.Flags().StringVar(&formURL, "url", "", "Direct URL to the opt-out form")
	cmd.Flags().BoolVar(&headless, "headless", true, "Run browser in headless mode (default: your config's pipeline.browser_headless, or true if unset)")
	cmd.Flags().BoolVar(&autoSubmit, "submit", false, "Automatically submit the form after filling")
	cmd.Flags().StringVar(&screenshotDir, "screenshots", "", "Directory to save screenshots (default: ~/.eraser/screenshots)")
	cmd.Flags().BoolVar(&pending, "pending", false, "Fill all pending forms from the pipeline")
	cmd.Flags().BoolVar(&waitForCaptcha, "wait", false, "Wait for user to solve CAPTCHA before continuing (use with --headless=false)")

	return cmd
}

func runFill(brokerID, formURL string, headless, headlessFlagSet, autoSubmit bool, screenshotDir string, pending bool, waitForCaptcha bool) error {
	// Load config for profile data
	cfg, err := config.Load(resolveConfigPath())
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	activeProfile, err := resolveProfile(cfg)
	if err != nil {
		return err
	}

	// Load brokers so we can build a domain allowlist -- NavigateAndFill
	// autofills PII into whatever page it's given, and form URLs can
	// originate from email-parsed content (untrusted), so we only want to
	// navigate to known broker domains.
	brokerDB, err := broker.LoadFromFile(resolveBrokerPath())
	if err != nil {
		return fmt.Errorf("failed to load brokers: %w", err)
	}

	// Build list of broker domains for validation (same logic as cmd_confirm.go)
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

	// The browser refuses to autofill PII when this list is empty (fail
	// closed). Catch that here so the failure is one clear message rather
	// than an identical per-form rejection with no stated cause - the list
	// comes out empty if the broker file parsed to zero entries, or if a
	// stub data/brokers.yaml in the current directory shadowed the real one.
	if len(brokerDomains) == 0 {
		return fmt.Errorf("no broker domains loaded from %s - refusing to autofill "+
			"personal data into pages that can't be checked against the broker list",
			resolveBrokerPath())
	}

	// --headless wins if you passed it explicitly; otherwise fall back to
	// the config's pipeline.browser_headless (true if that's unset too).
	if !headlessFlagSet {
		headless = cfg.Pipeline.Headless()
	}

	// Set default screenshot directory
	if screenshotDir == "" {
		home, _ := os.UserHomeDir()
		screenshotDir = filepath.Join(home, ".eraser", "screenshots")
	}

	// Initialize history store
	store, err := history.NewStore(history.DBPathFor(resolveConfigPath()))
	if err != nil {
		return fmt.Errorf("failed to initialize history: %w", err)
	}
	defer func() { _ = store.Close() }()

	// Create browser config
	browserCfg := browser.DefaultConfig()
	browserCfg.Headless = headless
	browserCfg.ScreenshotDir = screenshotDir
	if cfg.Pipeline.BrowserTimeoutSec > 0 {
		browserCfg.Timeout = time.Duration(cfg.Pipeline.BrowserTimeoutSec) * time.Second
	}

	// Set up wait for CAPTCHA if requested
	if waitForCaptcha {
		if headless {
			fmt.Println("⚠️  Warning: --wait requires --headless=false to be useful")
		}
		browserCfg.WaitForUser = true
		browserCfg.Timeout = 5 * time.Minute // Longer timeout when waiting for user
		browserCfg.WaitCallback = func() error {
			fmt.Println()
			fmt.Println("       ⏸️  CAPTCHA detected! Solve it in the browser window.")
			fmt.Println("       Press ENTER when done (or Ctrl+C to cancel)...")
			fmt.Println()
			reader := bufio.NewReader(os.Stdin)
			_, err := reader.ReadString('\n')
			return err
		}
	}

	// Create browser instance
	b, err := browser.New(browserCfg, &activeProfile.Profile, brokerDomains)
	if err != nil {
		return fmt.Errorf("failed to create browser: %w", err)
	}
	defer b.Close()

	fmt.Println("🌐 Browser Automation")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println()

	// Determine what to fill
	var formsToFill []struct {
		BrokerID string
		URL      string
	}

	if formURL != "" {
		// Direct URL provided
		formsToFill = append(formsToFill, struct {
			BrokerID string
			URL      string
		}{BrokerID: brokerID, URL: formURL})
	} else if brokerID != "" {
		// Get URL for specific broker from pipeline
		responses, err := store.GetBrokerResponses(activeProfile.ID, "form_required", false, 100)
		if err != nil {
			return fmt.Errorf("failed to get broker responses: %w", err)
		}

		found := false
		for _, resp := range responses {
			if resp.BrokerID == brokerID && resp.FormURL != "" {
				formsToFill = append(formsToFill, struct {
					BrokerID string
					URL      string
				}{BrokerID: resp.BrokerID, URL: resp.FormURL})
				found = true
				break
			}
		}

		if !found {
			return fmt.Errorf("no form URL found for broker: %s", brokerID)
		}
	} else if pending {
		// Get all pending forms
		responses, err := store.GetBrokerResponses(activeProfile.ID, "form_required", false, 100)
		if err != nil {
			return fmt.Errorf("failed to get broker responses: %w", err)
		}

		for _, resp := range responses {
			if resp.FormURL != "" {
				formsToFill = append(formsToFill, struct {
					BrokerID string
					URL      string
				}{BrokerID: resp.BrokerID, URL: resp.FormURL})
			}
		}

		if len(formsToFill) == 0 {
			fmt.Println("✅ No pending forms to fill")
			return nil
		}
	} else {
		return fmt.Errorf("please specify --url, --broker, or --pending")
	}

	fmt.Printf("📋 Forms to process: %d\n", len(formsToFill))
	fmt.Println()

	// Process each form
	for i, form := range formsToFill {
		fmt.Printf("[%d/%d] Processing %s\n", i+1, len(formsToFill), form.URL)

		if form.BrokerID != "" {
			fmt.Printf("       Broker: %s\n", form.BrokerID)
		}

		result, err := b.NavigateAndFill(form.URL, form.BrokerID, autoSubmit)
		if err != nil {
			fmt.Printf("       ❌ Error: %v\n", err)
			continue
		}

		// Print result
		if len(result.FieldsFilled) > 0 {
			fmt.Printf("       ✅ Filled fields: %s\n", strings.Join(result.FieldsFilled, ", "))
		}
		if len(result.FieldsMissing) > 0 {
			fmt.Printf("       ⚠️  Missing profile data for: %s\n", strings.Join(result.FieldsMissing, ", "))
		}

		if result.CaptchaFound {
			fmt.Printf("       🤖 CAPTCHA detected: %s\n", result.CaptchaType)

			// Store profile data as JSON for the helper page
			profileData := map[string]string{
				"email":     activeProfile.Email,
				"firstName": activeProfile.FirstName,
				"lastName":  activeProfile.LastName,
				"phone":     activeProfile.Phone,
				"address":   activeProfile.Address,
				"city":      activeProfile.City,
				"state":     activeProfile.State,
				"zipCode":   activeProfile.ZipCode,
				"country":   activeProfile.Country,
			}
			profileJSON, _ := json.Marshal(profileData)

			// Create pending task for CAPTCHA
			task := &history.PendingTask{
				ProfileID:    activeProfile.ID,
				BrokerID:     form.BrokerID,
				BrokerName:   form.BrokerID, // Will need broker lookup for proper name
				TaskType:     history.TaskCaptcha,
				FormURL:      form.URL,
				BrowserState: string(profileJSON), // Store profile data for helper page
				Status:       "pending",
			}
			if result.ScreenshotPath != "" {
				task.ScreenshotPath = result.ScreenshotPath
			}

			if err := store.AddPendingTask(task); err != nil {
				fmt.Printf("       ⚠️  Failed to create task: %v\n", err)
			} else {
				fmt.Printf("       📝 Created CAPTCHA task for manual solving\n")
			}

			// Update pipeline status
			_ = store.UpdatePipelineStatus(activeProfile.ID, form.BrokerID, history.PipelineAwaitingCaptcha)
		} else if result.SubmitAttempted {
			fmt.Printf("       📨 Form submitted!\n")
			_ = store.UpdatePipelineStatus(activeProfile.ID, form.BrokerID, history.PipelineFormFilled)
		} else if result.Success {
			fmt.Printf("       ✅ Form filled (not submitted)\n")
			_ = store.UpdatePipelineStatus(activeProfile.ID, form.BrokerID, history.PipelineFormFilled)
		}

		if result.ScreenshotPath != "" {
			fmt.Printf("       📸 Screenshot: %s\n", result.ScreenshotPath)
		}

		fmt.Println()

		// Small delay between forms to be respectful
		if i < len(formsToFill)-1 {
			time.Sleep(2 * time.Second)
		}
	}

	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Printf("✅ Processed %d forms\n", len(formsToFill))

	return nil
}
