package browser

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chromedp/cdproto/network"
)

// --- no Chrome needed ---------------------------------------------------

func TestNewAutomationSessionNeedsApprovedDomains(t *testing.T) {
	if _, err := NewAutomationSession(DefaultConfig(), testProfile(), URLPolicy{}); err == nil || !strings.Contains(err.Error(), "no approved domains") {
		t.Fatalf("a broker with no domains: %v", err)
	}
	// Chrome starts lazily, so building and closing a session needs no browser.
	s, err := NewAutomationSession(DefaultConfig(), testProfile(), NewURLPolicy("https://broker.example", ""))
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
}

func TestSnapshotFingerprintIgnoresEphemeralFields(t *testing.T) {
	a := Snapshot{ID: "one", Screenshot: "a.png", URL: "https://x.example/f", Text: "Remove me"}
	b := a
	b.ID, b.Screenshot = "two", "b.png"
	if a.Fingerprint() != b.Fingerprint() {
		t.Error("re-observing the same page changed its fingerprint")
	}
	b.Text = "Thank you"
	if a.Fingerprint() == b.Fingerprint() {
		t.Error("a changed page kept its fingerprint")
	}
}

func TestPolicyEdgeCases(t *testing.T) {
	if got := (URLPolicy{LocalOrigin: "http://127.0.0.1:9"}).connectSources(); got[len(got)-1] != "http://127.0.0.1:9" {
		t.Errorf("connect sources = %v, want the local origin included", got)
	}
	err := NewURLPolicy("https://does-not-resolve.invalid", "").Validate(context.Background(), "https://does-not-resolve.invalid/form")
	if err == nil || !strings.Contains(err.Error(), "cannot verify destination address") {
		t.Errorf("unresolvable host: %v", err)
	}
}

func TestOpenReportsChromeThatCannotStart(t *testing.T) {
	cfg := DefaultConfig()
	cfg.ExecutablePath = "/nonexistent/chrome"
	s, err := NewAutomationSession(cfg, testProfile(), URLPolicy{LocalOrigin: "http://127.0.0.1:1"})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.Open(context.Background(), "http://127.0.0.1:1/form"); err == nil || !strings.Contains(err.Error(), "start Chrome") {
		t.Fatalf("Open with no browser: %v", err)
	}
}

func TestNoteResponseStopsTheSessionOnPrivateAddresses(t *testing.T) {
	response := func(ip string) *network.EventResponseReceived {
		return &network.EventResponseReceived{Response: &network.Response{RemoteIPAddress: ip}}
	}
	for ip, blocked := range map[string]bool{"93.184.216.34": false, "": false, "10.0.0.5": true, "127.0.0.1": true, "169.254.169.254": true} {
		s := &AutomationSession{policy: NewURLPolicy("https://broker.example", "")}
		s.noteResponse(response(ip))
		if got := s.blockedReason() != ""; got != blocked {
			t.Errorf("response from %q: blocked = %v, want %v", ip, got, blocked)
		}
	}
	s := &AutomationSession{policy: NewURLPolicy("https://broker.example", "")}
	s.noteResponse(&network.EventResponseReceived{}) // no response attached
	if s.blockedReason() != "" {
		t.Error("an event with no response blocked the session")
	}
	// The demo's own loopback server is allowed.
	s = &AutomationSession{policy: URLPolicy{LocalOrigin: "http://127.0.0.1:9"}}
	s.noteResponse(response("127.0.0.1"))
	if s.blockedReason() != "" {
		t.Error("the demo's local server was blocked")
	}
}

// --- real Chrome --------------------------------------------------------

const controlsPage = `<!doctype html><title>Controls</title>
<form onsubmit="return false">
  <input name="nickname" aria-label="Nickname">
  <input type="checkbox" name="agree" aria-label="Agree">
  <select name="plan" aria-label="Plan"><option>Basic</option></select>
  <button type="button" id="go">Go</button>
  <button type="button" id="gone">Vanishing</button>
</form>`

// Each message comes from the page script or the Go wrapper in Act, and each is
// what an agent sees when its plan no longer matches the page.
func TestActReportsWhyAControlCannotBeUsed(t *testing.T) {
	s, base := automationFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = fmt.Fprint(w, controlsPage)
	}))
	ctx := context.Background()
	if _, err := s.Open(ctx, base); err != nil {
		t.Fatal(err)
	}
	pageCtx, cancel := s.operation(ctx) // evaluate needs the browser's own context
	defer cancel()
	observe := func() *Snapshot {
		t.Helper()
		snap, err := s.Observe(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return snap
	}
	mustFail := func(snap *Snapshot, label, action, value, want string) {
		t.Helper()
		_, err := s.Act(ctx, snap.ID, controlRef(t, snap, label), action, value)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s %s %q: error = %v, want it to mention %q", action, label, value, err, want)
		}
	}

	snap := observe()
	mustFail(snap, "Plan", "fill", "Enterprise", "Option not found")
	snap = observe() // a failed action leaves the snapshot usable, but be explicit
	mustFail(snap, "Agree", "fill", "maybe", "true or false")
	snap = observe()
	mustFail(snap, "Go", "fill", "text", "not a form field")
	snap = observe()
	mustFail(snap, "Go", "tap", "", "Unsupported action")

	// A control removed after the snapshot, and one disabled after it.
	snap = observe()
	gone, goRef := controlRef(t, snap, "Vanishing"), controlRef(t, snap, "Go")
	if err := s.evaluate(pageCtx, `document.getElementById('gone').remove(); document.getElementById('go').disabled = true; true`, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Act(ctx, snap.ID, gone, "click", ""); err == nil || !strings.Contains(err.Error(), "Control changed") {
		t.Errorf("removed control: %v", err)
	}
	if _, err := s.Act(ctx, snap.ID, goRef, "click", ""); err == nil || !strings.Contains(err.Error(), "Control unavailable") {
		t.Errorf("disabled control: %v", err)
	}
}

// The document CSP is what keeps a page from opening popups or starting
// workers that the request interceptor cannot see. Chrome ignores most of it
// when the header is only edited in place, so this checks it really applies.
func TestInjectedCSPApplies(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/worker.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript")
		_, _ = fmt.Fprint(w, `postMessage('ran')`)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = fmt.Fprint(w, `<!doctype html><title>starting</title><script>
const log = [window.open('about:blank') ? 'popup-opened' : 'popup-blocked'];
function done() { document.title = log.join(' '); }
try {
  const w = new Worker('/worker.js');
  w.onmessage = () => { log.push('worker-ran'); done(); };
  w.onerror = () => { log.push('worker-blocked'); done(); };
} catch (e) { log.push('worker-blocked'); done(); }
</script><button>Continue</button>`)
	})
	s, base := automationFixture(t, mux)
	ctx := context.Background()
	if _, err := s.Open(ctx, base); err != nil {
		t.Fatal(err)
	}
	// The page finishes asynchronously, whichever way the worker goes.
	var title string
	for range 40 {
		snap, err := s.Observe(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if title = snap.Title; title != "starting" {
			break
		}
	}
	if title != "popup-blocked worker-blocked" {
		t.Fatalf("title = %q, want popup-blocked worker-blocked: the injected CSP did not apply", title)
	}
}

func TestOpenReportsAPageThatCannotBeLoaded(t *testing.T) {
	b := requireChrome(t)
	b.config.ScreenshotDir = t.TempDir()
	web := httptest.NewServer(http.NotFoundHandler())
	url := web.URL
	web.Close() // nothing is listening any more
	s := &AutomationSession{b: b, policy: URLPolicy{LocalOrigin: url}}
	t.Cleanup(s.Close)
	if _, err := s.Open(context.Background(), url); err == nil || !strings.Contains(err.Error(), "open task page") {
		t.Fatalf("Open of an unreachable page: %v", err)
	}
}

func TestInjectedPolicyStringKeepsPopupsAndWorkersOut(t *testing.T) {
	csp := NewURLPolicy("https://broker.example", "").csp()
	for _, want := range []string{"sandbox ", "allow-forms", "worker-src 'none'", "connect-src 'self' https://broker.example https://www.broker.example"} {
		if !strings.Contains(csp, want) {
			t.Errorf("policy %q is missing %q", csp, want)
		}
	}
	if strings.Contains(csp, "allow-popups") || strings.Contains(csp, "allow-top-navigation") {
		t.Errorf("policy %q allows what the sandbox is there to stop", csp)
	}
}
