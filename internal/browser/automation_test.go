package browser

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/chromedp/chromedp"
)

func automationFixture(t *testing.T, h http.Handler) (*AutomationSession, string) {
	t.Helper()
	b := requireChrome(t)
	b.config.ScreenshotDir = t.TempDir()
	web := httptest.NewServer(h)
	t.Cleanup(web.Close)
	s := &AutomationSession{b: b, policy: URLPolicy{LocalOrigin: web.URL}}
	t.Cleanup(s.Close)
	return s, web.URL
}

func controlRef(t *testing.T, s *Snapshot, label string) string {
	t.Helper()
	for _, c := range s.Controls {
		if c.Label == label {
			return c.Ref
		}
	}
	t.Fatalf("control %q missing in %+v", label, s.Controls)
	return ""
}

func TestAutomationBrowserFillSelectSubmitAndStaleReferences(t *testing.T) {
	var received atomic.Int32
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		if r.URL.Path == "/done" {
			received.Add(1)
			_, _ = fmt.Fprint(w, "<h1>Your request has been received.</h1>")
			return
		}
		_, _ = fmt.Fprint(w, `<form method="post" action="/done"><label>Name<input name="name" required></label><label>Country<select><option>Norway</option><option>Sweden</option></select></label><button>Submit request</button></form>`)
	})
	s, url := automationFixture(t, h)
	ctx := context.Background()
	snap, err := s.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	old := snap.ID
	snap, err = s.Act(ctx, snap.ID, controlRef(t, snap, "Submit request"), "click", "")
	if err != nil || !snap.SubmissionBlocked || received.Load() != 0 || len(snap.Validation) == 0 {
		t.Fatalf("invalid form was not stopped before submission: %+v, %v", snap, err)
	}
	snap, err = s.Act(ctx, snap.ID, controlRef(t, snap, "Name"), "fill", `O'Connor "test"`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Act(ctx, old, "e0", "click", ""); err == nil {
		t.Fatal("stale reference accepted")
	}
	snap, err = s.Act(ctx, snap.ID, controlRef(t, snap, "Country"), "fill", "Sweden")
	if err != nil {
		t.Fatal(err)
	}
	if snap.Controls[1].Value != "Sweden" {
		t.Fatalf("select value: %+v", snap.Controls)
	}
	snap, err = s.Act(ctx, snap.ID, controlRef(t, snap, "Submit request"), "click", "")
	if err != nil {
		t.Fatal(err)
	}
	if received.Load() != 1 || !strings.Contains(snap.Text, "request has been received") {
		t.Fatalf("submission: %d, %+v", received.Load(), snap)
	}
	data, err := os.ReadFile(filepath.Join(s.b.config.ScreenshotDir, snap.Screenshot))
	if err != nil {
		t.Fatal(err)
	}
	if len(data) < 8 || string(data[:8]) != "\x89PNG\r\n\x1a\n" {
		t.Fatal("screenshot is not a PNG")
	}
}

func TestAutomationBlocksPopupAndKeepsDestinationPause(t *testing.T) {
	var hits atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1) }))
	defer other.Close()
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = fmt.Fprintf(w, `<button onclick="window.open('%s')">Open popup</button><img src="%s/track">`, other.URL, other.URL)
	})
	s, raw := automationFixture(t, h)
	ctx := context.Background()
	snap, err := s.Open(ctx, raw)
	if err != nil {
		t.Fatal(err)
	}
	if snap.Blocker == "" {
		t.Fatal("unexpected resource did not pause the page")
	}
	snap, err = s.Observe(ctx)
	if err != nil || snap.Blocker == "" {
		t.Fatalf("observing removed the destination pause: %+v, %v", snap, err)
	}
	// Exercise the browser guard directly; MCP already refuses actions while
	// needs_user is set. A script popup must not escape into another tab.
	if _, err := s.Act(ctx, snap.ID, controlRef(t, snap, "Open popup"), "click", ""); err != nil {
		t.Fatal(err)
	}
	if hits.Load() != 0 {
		t.Fatalf("unapproved destination received %d requests", hits.Load())
	}
}

func TestAutomationBlocksRedirectAndFormAction(t *testing.T) {
	var hits atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1) }))
	defer other.Close()
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, other.URL, http.StatusFound)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = fmt.Fprintf(w, `<form action="%s" method="post"><button>Submit</button></form>`, other.URL)
	})
	s, url := automationFixture(t, h)
	ctx := context.Background()
	snap, err := s.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = s.Act(ctx, snap.ID, controlRef(t, snap, "Submit"), "click", "")
	if hits.Load() != 0 {
		t.Fatal("form leaked to unapproved origin")
	}
	if err := chromedp.Run(s.b.ctx, chromedp.Navigate(url+"/redirect")); err == nil {
		t.Fatal("redirect was not blocked")
	}
	if hits.Load() != 0 {
		t.Fatal("redirect reached another origin")
	}
}

func TestAutomationHumanVerificationResume(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = fmt.Fprint(w, `<body><div id="gate"><p>Enter the security code: simulated human verification</p><input name="captcha"></div><button id="human" onclick="document.getElementById('gate').remove()">Complete simulated verification</button></body>`)
	})
	s, url := automationFixture(t, h)
	ctx := context.Background()
	snap, err := s.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	if snap.Blocker == "" {
		t.Fatal("missing handoff")
	}
	// Simulate the human completing our own local test fixture, not a CAPTCHA service.
	if err := chromedp.Run(s.b.ctx, chromedp.Click("#human")); err != nil {
		t.Fatal(err)
	}
	snap, err = s.Observe(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if snap.Blocker != "" {
		t.Fatalf("handoff did not resume: %s", snap.Blocker)
	}
}

func TestAutomationURLPolicy(t *testing.T) {
	for _, raw := range []string{"file:///etc/passwd", "javascript:alert(1)", "https://user:password@example.com", "http://example.com", "https://example.com.evil.test", "https://127.0.0.1", "https://10.0.0.1"} {
		p := URLPolicy{Domains: []string{"example.com", "127.0.0.1", "10.0.0.1"}}
		if err := p.Validate(context.Background(), raw); err == nil {
			t.Errorf("allowed %s", raw)
		}
	}
	if err := (URLPolicy{}).Validate(context.Background(), "https://example.com"); err == nil {
		t.Fatal("empty policy allowed navigation")
	}
}

func TestAutomationPausesForCredentialsAndIdentity(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = fmt.Fprint(w, `<label>Password<input type="password" value="secret-one"></label><label>Code<input autocomplete="one-time-code" value="secret-two"></label><input type="file"><button>Verify identity</button>`)
	})
	s, raw := automationFixture(t, h)
	snap, err := s.Open(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	if snap.Blocker == "" || !snap.SensitiveControls || snap.Screenshot != "" {
		t.Fatalf("sensitive page did not pause or exposed a screenshot: %+v", snap)
	}
	for _, c := range snap.Controls {
		if strings.Contains(c.Value, "secret-") {
			t.Fatal("credential returned in a control")
		}
	}
}

func TestURLPolicyExactHostsOnSharedPlatforms(t *testing.T) {
	platform := NewURLPolicy("https://example-broker.com", "https://privacyportal.onetrust.com/webform/abc")
	if !platform.allowsHost("privacyportal.onetrust.com") {
		t.Fatal("named tenant rejected")
	}
	if platform.allowsHost("other-company.onetrust.com") || platform.allowsHost("privacyportal.onetrust.com.evil.test") {
		t.Fatal("another tenant on a shared platform was allowed")
	}
	own := NewURLPolicy("https://example-broker.com", "https://optout.example-broker.com/form")
	if !own.allowsHost("forms.example-broker.com") || own.allowsHost("example-broker.com.attacker.test") {
		t.Fatal("own subdomain rule wrong")
	}
	mixed := NewURLPolicy("https://example-broker.com", "https://privacyportal.onetrust.com/x")
	if !mixed.allowsHost("www.example-broker.com") {
		t.Fatal("platform link removed the broker's own subdomains")
	}
}

func TestPublicAddress(t *testing.T) {
	for raw, want := range map[string]bool{"": true, "93.184.216.34": true, "10.0.0.1": false, "127.0.0.1": false, "169.254.1.1": false, "192.168.1.1": false, "not-an-ip": false} {
		if got := publicAddress(raw); got != want {
			t.Errorf("publicAddress(%q) = %v, want %v", raw, got, want)
		}
	}
}

func TestPathPinOnSharedPlatformsAndCSP(t *testing.T) {
	p := NewURLPolicy("https://broker.example", "https://privacyportal.onetrust.com/webform/abc/def")
	ctx := context.Background()
	if err := p.ValidateNavigation(ctx, "https://privacyportal.onetrust.com/webform/other/x"); err == nil {
		t.Error("another tenant's form on the same host was allowed")
	}
	for path, want := range map[string]bool{"/webform/abc": true, "/webform/abc/def": true, "/webform/abcd": false, "/": false, "/webform": false} {
		if got := p.pathAllowed("privacyportal.onetrust.com", path); got != want {
			t.Errorf("pathAllowed(%q) = %v, want %v", path, got, want)
		}
	}
	if !p.pathAllowed("broker.example", "/anything") {
		t.Error("an unpinned host was restricted by path")
	}
	if got := pathPrefix(""); got != "/" {
		t.Errorf("pathPrefix empty = %q", got)
	}
	src := strings.Join(p.connectSources(), " ")
	if !strings.Contains(src, "https://*.broker.example") || strings.Contains(src, "https://*.privacyportal.onetrust.com") {
		t.Errorf("CSP sources ignore the exact host rule: %s", src)
	}
}

func TestObserveRefusesPrivateAddressPage(t *testing.T) {
	s := &AutomationSession{networkBlocked: "private address"}
	if _, err := s.Observe(context.Background()); err == nil || !strings.Contains(err.Error(), "private address") {
		t.Fatalf("Observe on a blocked session: %v", err)
	}
}
