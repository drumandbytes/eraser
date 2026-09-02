package main

import (
	"fmt"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/drumandbytes/eraser/internal/broker"
	"github.com/spf13/cobra"
)

type auditVerdict string

const (
	verdictAlive       auditVerdict = "alive"
	verdictEmailDead   auditVerdict = "email-dead"
	verdictWebsiteDead auditVerdict = "website-dead"
	verdictUnknown     auditVerdict = "unknown"
	verdictSkipped     auditVerdict = "skipped"
)

// auditChecker is the network-touching part of the audit, injected so tests
// can exercise the verdict logic in auditOne without making real DNS/HTTP
// calls.
type auditChecker struct {
	lookupMX   func(domain string) ([]*net.MX, error)
	lookupHost func(domain string) ([]string, error)
	httpHead   func(url string) (*http.Response, error)
}

func newAuditChecker(timeout time.Duration) *auditChecker {
	client := &http.Client{
		Timeout: timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}
	return &auditChecker{
		lookupMX:   net.LookupMX,
		lookupHost: net.LookupHost,
		httpHead: func(url string) (*http.Response, error) {
			return client.Head(url)
		},
	}
}

func auditBrokersCmd() *cobra.Command {
	var region, category string
	var timeoutSec int

	cmd := &cobra.Command{
		Use:   "audit-brokers",
		Short: "Check broker websites and email domains for signs of life",
		Long: `Checks each broker's email domain (MX/host lookup) and website
(HTTP HEAD) to find entries that may have gone defunct - useful for
maintaining a large, hand-curated broker database over time.

This is read-only: it never modifies data/brokers.yaml. Use the reported
IDs to investigate and update the database manually - e.g. feed
email-dead ones into 'cleanup-bounces' (once bounce mail confirms it),
or look into a website-dead one before assuming it's actually gone.

A non-2xx/3xx website response is reported as "unknown" rather than dead -
many privacy-request pages block headless/bot requests, so an inconclusive
check isn't treated as evidence the broker is gone.

Examples:
  eraser audit-brokers
  eraser audit-brokers --region eu
  eraser audit-brokers --category people-search --timeout 15`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAuditBrokers(region, category, time.Duration(timeoutSec)*time.Second)
		},
	}

	cmd.Flags().StringVar(&region, "region", "", "Only audit brokers in this region")
	cmd.Flags().StringVar(&category, "category", "", "Only audit brokers in this category")
	cmd.Flags().IntVar(&timeoutSec, "timeout", 10, "Per-check timeout in seconds")

	return cmd
}

type auditResult struct {
	broker  broker.Broker
	verdict auditVerdict
}

func runAuditBrokers(region, category string, timeout time.Duration) error {
	brokerDB, err := broker.Load(brokerFile)
	if err != nil {
		return fmt.Errorf("failed to load brokers: %w", err)
	}

	region = strings.ToLower(strings.TrimSpace(region))
	category = strings.ToLower(strings.TrimSpace(category))

	var targets []broker.Broker
	for _, b := range brokerDB.Brokers {
		if region != "" && strings.ToLower(b.Region) != region {
			continue
		}
		if category != "" && strings.ToLower(b.Category) != category {
			continue
		}
		targets = append(targets, b)
	}

	fmt.Printf("🔍 Auditing %d broker(s)...\n", len(targets))
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	results := auditConcurrently(targets, newAuditChecker(timeout), 20)
	printAuditResults(results)

	return nil
}

// auditConcurrently runs auditOne for each broker with a bounded worker
// pool - checking 700+ brokers one at a time over DNS/HTTP would be slow.
func auditConcurrently(targets []broker.Broker, checker *auditChecker, workers int) []auditResult {
	results := make([]auditResult, len(targets))
	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup
	for i, b := range targets {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, b broker.Broker) {
			defer wg.Done()
			defer func() { <-sem }()
			results[i] = auditResult{broker: b, verdict: auditOne(b, checker)}
		}(i, b)
	}
	wg.Wait()
	return results
}

// auditOne classifies a single broker. email-dead takes priority over the
// website verdict when both are checked - a dead email address is the more
// actionable, more certain signal (an unreachable send target) than a
// website that merely failed to respond to a headless HEAD request.
func auditOne(b broker.Broker, checker *auditChecker) auditVerdict {
	hasEmail := b.Email != ""
	hasWebsite := b.Website != ""

	if !hasEmail && !hasWebsite {
		return verdictSkipped
	}

	if hasEmail && !checkEmailAlive(b.Email, checker) {
		return verdictEmailDead
	}

	if hasWebsite {
		return checkWebsiteVerdict(b.Website, checker)
	}

	// Email present and alive, no website to check.
	return verdictAlive
}

func checkEmailAlive(email string, checker *auditChecker) bool {
	at := strings.LastIndex(email, "@")
	if at < 0 || at == len(email)-1 {
		return false // malformed - can't be a valid mail destination regardless of DNS
	}
	domain := email[at+1:]

	if mxs, err := checker.lookupMX(domain); err == nil && len(mxs) > 0 {
		return true
	}
	// Some domains route mail without a dedicated MX record (implicit MX
	// via the A/AAAA record) - only call it dead if both lookups fail.
	if hosts, err := checker.lookupHost(domain); err == nil && len(hosts) > 0 {
		return true
	}
	return false
}

func checkWebsiteVerdict(website string, checker *auditChecker) auditVerdict {
	resp, err := checker.httpHead(website)
	if err != nil {
		return verdictWebsiteDead
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 200 && resp.StatusCode < 400 {
		return verdictAlive
	}
	// Many privacy/opt-out pages block bot/headless requests (403, etc.) -
	// that's not evidence the broker is gone, just an inconclusive check.
	return verdictUnknown
}

func printAuditResults(results []auditResult) {
	counts := map[auditVerdict]int{}
	var emailDead, websiteDead []string
	for _, r := range results {
		counts[r.verdict]++
		switch r.verdict {
		case verdictEmailDead:
			emailDead = append(emailDead, r.broker.ID)
		case verdictWebsiteDead:
			websiteDead = append(websiteDead, r.broker.ID)
		}
	}

	fmt.Println()
	fmt.Printf("✓ alive: %d\n", counts[verdictAlive])
	fmt.Printf("✗ email-dead: %d\n", counts[verdictEmailDead])
	fmt.Printf("✗ website-dead: %d\n", counts[verdictWebsiteDead])
	fmt.Printf("? unknown: %d\n", counts[verdictUnknown])
	fmt.Printf("- skipped: %d\n", counts[verdictSkipped])

	if len(emailDead) > 0 {
		sort.Strings(emailDead)
		fmt.Println()
		fmt.Println("Email domain appears dead (MX and host lookup both failed):")
		for _, id := range emailDead {
			fmt.Printf("  - %s\n", id)
		}
	}
	if len(websiteDead) > 0 {
		sort.Strings(websiteDead)
		fmt.Println()
		fmt.Println("Website appears unreachable:")
		for _, id := range websiteDead {
			fmt.Printf("  - %s\n", id)
		}
	}
}
