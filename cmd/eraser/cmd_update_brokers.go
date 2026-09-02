package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/drumandbytes/eraser/internal/broker"
	"github.com/spf13/cobra"
)

const defaultBrokersURL = "https://raw.githubusercontent.com/drumandbytes/eraser/main/data/brokers.yaml"

// minSaneBrokerCount guards against replacing the local list with a truncated
// or empty download (a proxy error page, a bad commit, ...).
const minSaneBrokerCount = 200

func updateBrokersCmd() *cobra.Command {
	var (
		url   string
		check bool
	)

	cmd := &cobra.Command{
		Use:   "update-brokers",
		Short: "Fetch the latest broker list from the public repository",
		Long: `Download a fresh data/brokers.yaml from the public repo into
~/.eraser/brokers.yaml, which then takes precedence over the copy built into
this binary. The request is conditional (If-None-Match): if nothing has
changed it costs a few hundred bytes and writes nothing.

The app itself is not updated - only the broker list. Never runs automatically.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUpdateBrokers(url, check)
		},
	}

	cmd.Flags().StringVar(&url, "url", defaultBrokersURL, "source URL for brokers.yaml")
	cmd.Flags().BoolVar(&check, "check", false, "only report whether an update is available (exit 1 if so); write nothing")

	return cmd
}

func brokersETagPath() string {
	return filepath.Join(filepath.Dir(broker.UserBrokersPath()), "brokers.etag")
}

func currentBrokerCount() int {
	db, err := broker.Load("")
	if err != nil {
		return 0
	}
	return len(db.Brokers)
}

func runUpdateBrokers(url string, check bool) error {
	etagPath := brokersETagPath()
	savedETag, _ := os.ReadFile(etagPath)

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("bad URL: %w", err)
	}
	if len(savedETag) > 0 {
		req.Header.Set("If-None-Match", strings.TrimSpace(string(savedETag)))
	}
	req.Header.Set("User-Agent", "eraser-update-brokers")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusNotModified:
		fmt.Printf("✓ Broker list is up to date (%d entries).\n", currentBrokerCount())
		return nil
	case http.StatusOK:
		// handled below
	default:
		return fmt.Errorf("unexpected response: %s", resp.Status)
	}

	if check {
		fmt.Println("⬆️  A newer broker list is available. Run `eraser update-brokers` to fetch it.")
		os.Exit(1)
		return nil
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}
	db, err := broker.Parse(body)
	if err != nil {
		return fmt.Errorf("downloaded file is not valid broker YAML: %w", err)
	}
	if len(db.Brokers) < minSaneBrokerCount {
		return fmt.Errorf("downloaded list has only %d entries (expected at least %d) - refusing to replace the local copy", len(db.Brokers), minSaneBrokerCount)
	}

	before := currentBrokerCount()
	target := broker.UserBrokersPath()
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return fmt.Errorf("failed to create %s: %w", filepath.Dir(target), err)
	}
	if err := os.WriteFile(target, body, 0o644); err != nil {
		return fmt.Errorf("failed to write %s: %w", target, err)
	}
	if etag := resp.Header.Get("ETag"); etag != "" {
		_ = os.WriteFile(etagPath, []byte(etag), 0o644)
	}

	fmt.Println("⬇️  Broker list updated")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Printf("   %s\n", target)
	fmt.Printf("   %d entries (was %d)\n", len(db.Brokers), before)
	return nil
}
