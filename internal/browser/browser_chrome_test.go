package browser

import (
	"context"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/drumandbytes/eraser/internal/config"
)

func testProfile() *config.Profile {
	return &config.Profile{
		FirstName: "Test",
		LastName:  "User",
		Email:     "test@example.com",
	}
}

// The allowlist check runs before any chromedp.Run, and New() doesn't launch
// Chrome, so this needs no browser binary.
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

// Subdomains must pass the allowlist; any error must be navigation/timeout,
// not the domain rejection.
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

// Mirrors clickByTextJS's inline JS regex in Go to pin its semantics; a proxy,
// not a substitute for TestSubmitForm_ClicksButtonByVisibleText.
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

// Ubuntu 24.04 runners block the unprivileged user namespaces Chrome's
// sandbox needs. The pages under test are local fixtures, so CI runs
// without it.
func init() {
	if os.Getenv("GITHUB_ACTIONS") == "true" {
		extraAllocatorOptions = append(extraAllocatorOptions, chromedp.NoSandbox)
	}
}

// requireChrome probes with a trivial navigation (Chrome launches lazily on the
// first Run) and skips the test when no browser can start.
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
		// GitHub's runners ship Chrome: there a missing browser is a broken
		// setup, not a reason to quietly skip the browser tests.
		if os.Getenv("GITHUB_ACTIONS") == "true" {
			t.Fatalf("Chrome unavailable in CI: %v", err)
		}
		t.Skipf("no usable Chrome/Chromium binary in this environment, skipping: %v", err)
	}

	return b
}

// End to end: a button with no submit-ish selector but matching text gets clicked.
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

// One page per detector branch, plus a clean page, so the single detection
// script keeps each check and its order.
func TestDetectCaptcha(t *testing.T) {
	b := requireChrome(t)
	defer b.Close()

	cases := []struct{ name, body, want string }{
		{"none", `<form><input name="email"></form>`, ""},
		{"recaptcha v2", `<div class="g-recaptcha" data-sitekey="x"></div>`, CaptchaTypeRecaptchaV2},
		{"recaptcha v3", `<script src="about:blank?recaptcha&render=abc"></script>`, CaptchaTypeRecaptchaV3},
		{"hcaptcha", `<div class="h-captcha"></div>`, CaptchaTypeHCaptcha},
		{"turnstile", `<div class="cf-turnstile"></div>`, CaptchaTypeTurnstile},
		{"funcaptcha", `<div id="FunCaptcha"></div>`, CaptchaTypeFunCaptcha},
		{"cloudflare", `<title>Just a moment...</title><p>checking</p>`, CaptchaTypeCloudflare},
		{"text captcha", `<p>Enter the code shown</p><input name="captcha_answer">`, CaptchaTypeTextCaptcha},
		{"image captcha", `<img src="/captcha.png" alt="x">`, CaptchaTypeImageCaptcha},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(b.ctx, b.config.Timeout)
			defer cancel()
			if err := chromedp.Run(ctx, chromedp.Navigate("data:text/html,<html><head></head><body>"+tc.body+"</body></html>")); err != nil {
				t.Fatalf("navigate: %v", err)
			}
			got, err := b.detectCaptcha(ctx)
			if err != nil {
				t.Fatalf("detectCaptcha: %v", err)
			}
			if got.Type != tc.want || got.Found != (tc.want != "") {
				t.Fatalf("got %+v, want type %q", got, tc.want)
			}
		})
	}
}
