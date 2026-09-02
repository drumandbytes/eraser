package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/eraser-privacy/eraser/internal/broker"
	"github.com/eraser-privacy/eraser/internal/config"
	emailtmpl "github.com/eraser-privacy/eraser/internal/template"
	"github.com/spf13/cobra"
)

func draftCmd() *cobra.Command {
	var (
		output   string
		region   string
		category string
	)

	cmd := &cobra.Command{
		Use:   "draft [broker-id ...]",
		Short: "Render removal emails for you to send by hand",
		Long: `Render the removal request email(s) so you can send them yourself from
your own mail client - no SMTP credentials needed. After you send one, record
it with 'eraser mark-sent <broker-id>'.

With no -o, prints one broker's email (recipient, subject, body) to the
terminal. With -o <dir>, writes one .eml file per broker that opens straight
into Mail.app / Thunderbird / Outlook, already addressed.

Examples:
  eraser draft spokeo                 # print Spokeo's email
  eraser draft --region eu -o ./out   # one .eml per EU broker
  eraser draft -o ./out               # one .eml per broker`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDraft(args, output, region, category)
		},
	}

	cmd.Flags().StringVarP(&output, "output", "o", "", "write one .eml file per broker into this directory")
	cmd.Flags().StringVar(&region, "region", "", "only brokers in this region (us, eu, global)")
	cmd.Flags().StringVar(&category, "category", "", "only brokers in this category")

	return cmd
}

func runDraft(args []string, output, region, category string) error {
	cfg, err := config.Load(resolveConfigPath())
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	activeProfile, err := resolveProfile(cfg)
	if err != nil {
		return err
	}

	brokerDB, err := broker.LoadFromFile(resolveBrokerPath())
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

	engine, err := emailtmpl.NewEngine()
	if err != nil {
		return fmt.Errorf("failed to init template engine: %w", err)
	}
	tmpl := cfg.Options.Template
	if tmpl == "" {
		tmpl = "gdpr"
	}

	from := cfg.Email.From
	if from == "" {
		from = activeProfile.Email
	}

	// stdout mode: only sensible for a single broker.
	if output == "" {
		if len(brokers) != 1 {
			return fmt.Errorf("printing to the terminal only works for one broker; pass a single id, or use -o <dir> for %d brokers", len(brokers))
		}
		b := brokers[0]
		if strings.TrimSpace(b.Email) == "" {
			fmt.Printf("⚠️  %s has no email on file", b.Name)
			if b.OptOutURL != "" {
				fmt.Printf(" - use the opt-out form: %s", b.OptOutURL)
			}
			fmt.Println()
		}
		email, err := engine.Render(tmpl, activeProfile.Profile, b)
		if err != nil {
			return fmt.Errorf("failed to render email for %s: %w", b.ID, err)
		}
		fmt.Printf("To: %s\n", b.Email)
		fmt.Printf("Subject: %s\n\n", email.Subject)
		fmt.Println(email.Body)
		fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
		fmt.Printf("After sending: eraser mark-sent %s\n", b.ID)
		return nil
	}

	if err := os.MkdirAll(output, 0o700); err != nil {
		return fmt.Errorf("failed to create %s: %w", output, err)
	}

	written, skipped := 0, 0
	for _, b := range brokers {
		if strings.TrimSpace(b.Email) == "" {
			skipped++
			continue
		}
		email, err := engine.Render(tmpl, activeProfile.Profile, b)
		if err != nil {
			return fmt.Errorf("failed to render email for %s: %w", b.ID, err)
		}
		path := filepath.Join(output, b.ID+".eml")
		if err := os.WriteFile(path, formatEML(from, b.Email, email), 0o600); err != nil {
			return fmt.Errorf("failed to write %s: %w", path, err)
		}
		written++
	}

	fmt.Println("✉️  Drafts written")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Printf("   %d .eml file(s) in %s\n", written, output)
	if skipped > 0 {
		fmt.Printf("   %d broker(s) skipped (no email on file - use their opt-out form)\n", skipped)
	}
	fmt.Println("   Open each in your mail client, send it, then: eraser mark-sent <broker-id>")
	return nil
}
