package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/eraser-privacy/eraser/internal/config"
	"github.com/eraser-privacy/eraser/internal/template"
	"github.com/spf13/cobra"
)

func initCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Initialize configuration interactively",
		Long:  "Create a new configuration file with your personal information and email settings.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInit()
		},
	}
}

func runInit() error {
	reader := bufio.NewReader(os.Stdin)
	configPath := resolveConfigPath()

	// If a config already exists, load it so every prompt below can offer
	// the current value as a default - re-running init then becomes an
	// update flow (hit Enter to keep, type a new value to change it)
	// instead of starting from a blank slate.
	var existing config.Config
	updating := false
	if _, statErr := os.Stat(configPath); statErr == nil {
		if loaded, err := config.Load(configPath); err == nil {
			existing = *loaded
			updating = true
		}
	}

	if updating {
		fmt.Println("🔐 Eraser Configuration Update")
		fmt.Println("===============================")
		fmt.Println()
		fmt.Printf("Found existing config at %s.\n", configPath)
		fmt.Println("Press Enter on any question to keep its current value, shown in [brackets].")
		fmt.Println()
	} else {
		fmt.Println("🔐 Eraser Configuration Setup")
		fmt.Println("==============================")
		fmt.Println()
	}

	cfg := &config.Config{}

	// Profile
	fmt.Println("📋 Personal Information (used in removal requests)")
	fmt.Println()

	cfg.Profile.FirstName = promptWithDefault(reader, "First name", existing.Profile.FirstName)
	cfg.Profile.MiddleName = promptWithDefault(reader, "Middle name (optional)", existing.Profile.MiddleName)
	cfg.Profile.LastName = promptWithDefault(reader, "Last name", existing.Profile.LastName)
	nameVariants := promptWithDefault(reader,
		"Other spellings of your name brokers might have, e.g. without diacritics - comma separated (optional)",
		strings.Join(existing.Profile.NameVariants, ", "))
	cfg.Profile.NameVariants = splitAndTrim(nameVariants)
	cfg.Profile.Email = promptWithDefault(reader, "Email address", existing.Profile.Email)
	otherEmails := promptWithDefault(reader,
		"Other email addresses you've used over the years - comma separated (optional)",
		strings.Join(existing.Profile.AdditionalEmails, ", "))
	cfg.Profile.AdditionalEmails = splitAndTrim(otherEmails)
	cfg.Profile.Address = promptWithDefault(reader, "Street address (optional)", existing.Profile.Address)
	cfg.Profile.City = promptWithDefault(reader, "City (optional)", existing.Profile.City)
	cfg.Profile.State = promptWithDefault(reader, "State/Province (optional)", existing.Profile.State)
	cfg.Profile.ZipCode = promptWithDefault(reader, "ZIP/Postal code (optional)", existing.Profile.ZipCode)
	cfg.Profile.Country = promptWithDefault(reader, "Country (optional)", existing.Profile.Country)
	prevAddresses := promptWithDefault(reader,
		"Previous address(es) from the last 5-7 years, if different - semicolon separated (optional)",
		strings.Join(existing.Profile.PreviousAddresses, "; "))
	cfg.Profile.PreviousAddresses = splitAndTrimBy(prevAddresses, ";")
	cfg.Profile.Phone = promptWithDefault(reader, "Phone number (optional)", existing.Profile.Phone)
	otherPhones := promptWithDefault(reader,
		"Other phone numbers you've used - comma separated (optional)",
		strings.Join(existing.Profile.AdditionalPhones, ", "))
	cfg.Profile.AdditionalPhones = splitAndTrim(otherPhones)

	fmt.Println()
	fmt.Println("📧 Email Settings")
	fmt.Println()

	cfg.Email.Provider = "smtp"
	cfg.Email.From = cfg.Profile.Email

	fmt.Println()
	fmt.Println("Gmail SMTP Configuration:")
	fmt.Println("  (See https://support.google.com/accounts/answer/185833 for app password setup)")
	fmt.Println()
	cfg.Email.SMTP.Host = "smtp.gmail.com"
	cfg.Email.SMTP.Port = 465
	cfg.Email.SMTP.UseTLS = true
	cfg.Email.SMTP.Username = promptWithDefault(reader, "  Gmail address", existing.Email.SMTP.Username)
	cfg.Email.SMTP.Password = promptSecretWithDefault(reader, "  App password (16-character code)", existing.Email.SMTP.Password)

	fmt.Println()
	fmt.Println("⚙️  Options")
	fmt.Println()

	defaultTemplate := existing.Options.Template
	if defaultTemplate == "" {
		// This fork is customized for GDPR Article 17 use (see EU-NOTES.md);
		// upstream defaulted fresh configs to "generic" for its US/CCPA
		// audience. An existing config's own template choice always wins.
		defaultTemplate = "gdpr"
	}
	fmt.Println("  Templates:")
	fmt.Println("    gdpr        - EU GDPR erasure (Article 17)")
	fmt.Println("    ccpa        - California CCPA deletion")
	fmt.Println("    generic     - references several privacy laws, works anywhere")
	fmt.Println("    uk-access   - UK GDPR subject access request (Article 15) - asks what they hold")
	fmt.Println("    uk-erasure  - UK GDPR erasure (Article 17)")
	fmt.Println("    uk-combined - UK GDPR: both, with the access request answered first")
	fmt.Println()
	fmt.Println("  UK residents: the uk-* templates cite UK GDPR and the ICO rather than")
	fmt.Println("  EU GDPR. Sending uk-access before uk-erasure is usually worth it - the")
	fmt.Println("  reply names who they bought your data from and who they sold it to,")
	fmt.Println("  which a broker can no longer tell you once it has erased your record.")
	fmt.Println()
	cfg.Options.Template = promptWithDefault(reader, "Default template ("+strings.Join(template.TemplateNames(), "/")+")", defaultTemplate)

	// Carry forward tuning options rather than resetting them, so a
	// hand-edited rate_limit_ms/daily_send_limit in the YAML survives
	// re-running init to update other fields.
	cfg.Options.RateLimitMs = existing.Options.RateLimitMs
	if cfg.Options.RateLimitMs == 0 {
		cfg.Options.RateLimitMs = 2000
	}
	cfg.Options.DailySendLimit = existing.Options.DailySendLimit
	cfg.Options.Regions = existing.Options.Regions
	cfg.Options.ExcludedBrokers = existing.Options.ExcludedBrokers
	cfg.Options.ExcludedCategories = existing.Options.ExcludedCategories

	// Carry forward inbox/pipeline settings too - init only manages the
	// profile/email/template fields above.
	cfg.Inbox = existing.Inbox
	cfg.Pipeline = existing.Pipeline

	// Carry forward any additional named profiles added via 'eraser profile
	// add' - re-running init only updates the primary/legacy profile, it
	// must not silently drop the others. If a "default" entry exists among
	// them, keep it in sync with the primary profile fields just entered
	// above instead of leaving it stale.
	cfg.Profiles = make([]config.NamedProfile, len(existing.Profiles))
	copy(cfg.Profiles, existing.Profiles)
	for i := range cfg.Profiles {
		if strings.EqualFold(cfg.Profiles[i].ID, config.DefaultProfileID) {
			cfg.Profiles[i].Profile = cfg.Profile
		}
	}

	if err := config.Save(configPath, cfg); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	fmt.Println()
	if updating {
		fmt.Printf("✅ Configuration updated: %s\n", configPath)
	} else {
		fmt.Printf("✅ Configuration saved to: %s\n", configPath)
	}
	fmt.Println()
	fmt.Println("Next steps:")
	fmt.Println("  1. Review and edit the config file if needed")
	fmt.Println("  2. Run 'eraser list-brokers' to see available brokers")
	fmt.Println("  3. Run 'eraser send --dry-run' to preview emails")
	fmt.Println("  4. Run 'eraser send' to send removal requests")

	return nil
}

// promptWithDefault shows the current value (if any) in brackets and, if the
// user just hits Enter, keeps it instead of blanking the field out. For list
// fields, pass the already-joined display string as current and re-split
// whatever comes back (unchanged or not) the same way as on first entry.
func promptWithDefault(reader *bufio.Reader, label, current string) string {
	msg := label
	if current != "" {
		msg = fmt.Sprintf("%s [%s]", label, current)
	}
	input := prompt(reader, msg+": ")
	if input == "" {
		return current
	}
	return input
}

// promptSecretWithDefault is like promptWithDefault but never echoes the
// existing value back (for passwords) - it just notes one is already set.
func promptSecretWithDefault(reader *bufio.Reader, label, current string) string {
	msg := label
	if current != "" {
		msg = label + " (leave blank to keep current)"
	}
	input := prompt(reader, msg+": ")
	if input == "" {
		return current
	}
	return input
}

// splitAndTrim splits a comma-separated string into a trimmed, non-empty slice.
// Returns nil for blank input.
func splitAndTrim(s string) []string {
	return splitAndTrimBy(s, ",")
}

// splitAndTrimBy splits s on sep into a trimmed, non-empty slice. Returns nil
// for blank input. Use a non-comma separator (e.g. ";") for values - like
// addresses - that may themselves contain commas.
func splitAndTrimBy(s, sep string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, sep)
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}
