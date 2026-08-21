package browser

import (
	"context"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/eraser-privacy/eraser/internal/config"
)

func testProfile() *config.Profile {
	return &config.Profile{
		FirstName: "Test",
		LastName:  "User",
		Email:     "test@example.com",
	}
}

// NavigateAndFill's domain allowlist check (browser.go) runs before any
// chromedp call is made -- it operates purely on the URL string and
// b.allowedDomains, and returns early on a mismatch well before `ctx` (which
// wraps b.ctx) is even created. That means this regression test doesn't need
// a real Chrome/Chromium binary at all: browser.New() itself only wires up
// chromedp's allocator/context options without spawning a browser process
// (chromedp launches lazily on the first chromedp.Run), and this path never
// reaches a chromedp.Run call.
func TestNavigateAndFill_RejectsDisallowedDomainBeforeTouchingBrowser(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Headless = true
	cfg.Timeout = 5 * time.Second

	b, err := New(cfg, testProfile(), []string{"broker.example.com"})
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer b.Close()

	result, err := b.NavigateAndFill("https://evil.example.com/optout", "test-broker", false)
	if err == nil {
		t.Fatal("expected an error for a URL whose domain is not in the allowlist, got nil")
	}
	if !strings.Contains(err.Error(), "not in known broker list") {
		t.Errorf("error = %v, want a domain-allowlist rejection message", err)
	}
	if result == nil {
		t.Fatal("result is nil")
	}
	if result.ErrorMessage == "" {
		t.Error("result.ErrorMessage is empty; want it populated with the rejection reason")
	}
	if result.Success {
		t.Error("result.Success = true for a rejected domain")
	}
}

// A subdomain of an allowed domain must still be accepted by the same check
// (matchesAllowedDomain's subdomain-suffix rule) -- this only verifies the
// check doesn't reject a legitimate case; it doesn't reach real navigation
// (Timeout is effectively irrelevant since we never get past the allowlist
// check without a reachable page, so we use an unroutable target and just
// confirm the *error*, if any, is a navigation/timeout failure rather than
// the domain-rejection message).
func TestNavigateAndFill_AllowsSubdomainOfAllowedDomain(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Headless = true
	cfg.Timeout = 2 * time.Second

	b, err := New(cfg, testProfile(), []string{"broker.example.com"})
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer b.Close()

	_, err = b.NavigateAndFill("https://forms.broker.example.com/optout", "test-broker", false)
	if err != nil && strings.Contains(err.Error(), "not in known broker list") {
		t.Errorf("a subdomain of an allowed domain was rejected by the allowlist check: %v", err)
	}
	// Whatever happens past this point (navigation failure, timeout, or a
	// Chrome launch failure in this environment) is out of scope for this
	// test -- we only care that the allowlist check itself didn't reject a
	// legitimate subdomain.
}

// submitForm's clickByTextJS regex (`/submit|remove|opt.?out|delete|request/i`)
// is embedded inline in a JS snippet passed to chromedp.Evaluate, so it isn't
// independently callable from Go. This test mirrors that exact pattern as a
// Go regexp and checks it against representative button labels, to pin down
// the intended matching semantics (and catch an accidental change to the
// pattern) even where a full browser-based test isn't run. It is a proxy for
// the JS behavior, not a substitute for TestSubmitForm_ClicksButtonByVisibleText
// below.
func TestSubmitButtonTextPattern_MirrorsJSRegex(t *testing.T) {
	// Keep in sync with the pattern in submitForm's clickByTextJS (browser.go).
	pattern := regexp.MustCompile(`(?i)submit|remove|opt.?out|delete|request`)

	tests := []struct {
		text      string
		wantMatch bool
	}{
		{"Submit", true},
		{"Submit Request", true},
		{"Remove My Information", true},
		{"Opt Out", true},
		{"Opt-Out", true},
		{"Delete My Data", true},
		{"Request Removal", true},
		{"SUBMIT", true},
		{"Cancel", false},
		{"Learn More", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.text, func(t *testing.T) {
			if got := pattern.MatchString(tt.text); got != tt.wantMatch {
				t.Errorf("pattern.MatchString(%q) = %v, want %v", tt.text, got, tt.wantMatch)
			}
		})
	}
}

// requireChrome creates a Browser and probes it with a trivial navigation to
// confirm a real Chrome/Chromium binary is actually launchable in this
// environment. chromedp.NewExecAllocator/NewContext (called from New) don't
// spawn a browser process by themselves -- the process only launches lazily
// on the first chromedp.Run -- so this is the earliest point a missing-Chrome
// environment can be detected. Tests that need a live browser call this and
// get a Browser back, or the test is skipped cleanly.
func requireChrome(t *testing.T) *Browser {
	t.Helper()

	cfg := DefaultConfig()
	cfg.Headless = true
	cfg.Timeout = 15 * time.Second

	b, err := New(cfg, testProfile(), nil)
	if err != nil {
		t.Skipf("browser.New failed, skipping Chrome-dependent test: %v", err)
	}

	ctx, cancel := context.WithTimeout(b.ctx, 10*time.Second)
	defer cancel()
	if err := chromedp.Run(ctx, chromedp.Navigate("about:blank")); err != nil {
		b.Close()
		t.Skipf("no usable Chrome/Chromium binary in this environment, skipping: %v", err)
	}

	return b
}

// End-to-end check that submitForm's text-matching fallback (clickByTextJS)
// actually finds and clicks a button that has no submit-ish CSS selector
// (type=submit, .submit-button, #submit, #submit-btn) but does have matching
// visible text -- this is exactly the case the old ":contains('Submit')"
// selectors silently failed to handle, since that isn't valid CSS and
// querySelector throws for it every time.
func TestSubmitForm_ClicksButtonByVisibleText(t *testing.T) {
	b := requireChrome(t)
	defer b.Close()

	const page = `data:text/html,<html><body>` +
		`<button id="rmv" onclick="document.title='clicked'">Remove My Data</button>` +
		`</body></html>`

	ctx, cancel := context.WithTimeout(b.ctx, b.config.Timeout)
	defer cancel()

	if err := chromedp.Run(ctx, chromedp.Navigate(page)); err != nil {
		t.Fatalf("navigate failed: %v", err)
	}

	if err := b.submitForm(ctx); err != nil {
		t.Fatalf("submitForm returned an error: %v", err)
	}

	var title string
	if err := chromedp.Run(ctx, chromedp.Title(&title)); err != nil {
		t.Fatalf("could not read page title: %v", err)
	}
	if title != "clicked" {
		t.Errorf("document.title = %q after submitForm; want %q (button was not clicked by text match)", title, "clicked")
	}
}
