package main

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/drumandbytes/eraser/internal/broker"
	"github.com/drumandbytes/eraser/internal/config"
	"github.com/drumandbytes/eraser/internal/evidence"
	"github.com/drumandbytes/eraser/internal/history"
	emailtmpl "github.com/drumandbytes/eraser/internal/template"
	"github.com/spf13/cobra"
)

func exportCmd() *cobra.Command {
	var (
		output string
		format string
		since  string
	)

	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export an evidence report of every removal request and response",
		Long: `Build a single document recording, per broker: what removal request was
sent (date, recipient, legal basis, a reconstruction of the email), what the
broker replied and when, the current pipeline stage, and whether the statutory
response deadline has passed with no substantive reply.

Intended as the attachment for a complaint to a data protection authority or to
noyb.eu when a controller ignores a GDPR Article 17 request. HTML opens in a
browser and prints to PDF; JSON is the same data unformatted.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runExport(exportOptions{output: output, format: format, since: since})
		},
	}

	cmd.Flags().StringVarP(&output, "output", "o", "", "output file (default: eraser-evidence-<profile>-<date>.<ext>)")
	cmd.Flags().StringVar(&format, "format", "html", "output format: html or json")
	cmd.Flags().StringVar(&since, "since", "", "only include requests sent on or after this date (YYYY-MM-DD)")

	return cmd
}

type exportOptions struct {
	output string
	format string
	since  string
}

func runExport(opts exportOptions) error {
	switch opts.format {
	case "html", "json":
	default:
		return fmt.Errorf("unknown --format %q (want html or json)", opts.format)
	}

	var sinceTime time.Time
	if opts.since != "" {
		t, err := time.Parse("2006-01-02", opts.since)
		if err != nil {
			return fmt.Errorf("invalid --since %q (want YYYY-MM-DD): %w", opts.since, err)
		}
		sinceTime = t
	}

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
		return fmt.Errorf("failed to open history: %w", err)
	}
	defer func() { _ = store.Close() }()

	requests, err := store.GetAllRequests(activeProfile.ID)
	if err != nil {
		return fmt.Errorf("failed to read requests: %w", err)
	}
	responses, err := store.GetBrokerResponsesForExport(activeProfile.ID)
	if err != nil {
		return fmt.Errorf("failed to read responses: %w", err)
	}

	brokerDB, err := broker.Load(brokerFile)
	if err != nil {
		return fmt.Errorf("failed to load brokers: %w", err)
	}
	engine, err := emailtmpl.NewEngine()
	if err != nil {
		return fmt.Errorf("failed to init template engine: %w", err)
	}

	report := evidence.Build(activeProfile.Profile, requests, responses, brokerDB, engine, sinceTime, time.Now())

	path := opts.output
	if path == "" {
		ext := opts.format
		path = fmt.Sprintf("eraser-evidence-%s-%s.%s", activeProfile.ID, time.Now().Format("2006-01-02"), ext)
	}

	var data []byte
	if opts.format == "json" {
		data, err = json.MarshalIndent(report, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to encode JSON: %w", err)
		}
	} else {
		data, err = evidence.RenderHTML(report)
		if err != nil {
			return fmt.Errorf("failed to render HTML: %w", err)
		}
	}

	// The report contains the data subject's identity details - same 0600 as
	// every other personal-data file this tool writes.
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("failed to write %s: %w", path, err)
	}

	fmt.Println("📄 Evidence report written")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	if len(cfg.GetProfiles()) > 1 {
		fmt.Printf("👤 Profile: %s\n", activeProfile.ID)
	}
	fmt.Printf("   File:     %s\n", path)
	fmt.Printf("   Brokers:  %d contacted, %d replied\n", report.Summary.BrokersContacted, report.Summary.BrokersResponded)
	fmt.Printf("   Requests: %d sent, %d failed\n", report.Summary.Sent, report.Summary.Failed)
	if n := len(report.Summary.PastDeadline); n > 0 {
		fmt.Printf("   ⚠️  %d broker(s) past the response deadline with no substantive reply:\n", n)
		for _, name := range report.Summary.PastDeadline {
			fmt.Printf("        - %s\n", name)
		}
		if report.Authority != nil {
			fmt.Printf("   Complain to: %s\n", report.Authority.Describe())
		}
	}
	return nil
}
