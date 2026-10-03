package browser

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/drumandbytes/eraser/internal/config"
)

// optOutSite serves one opt-out page and records the submitted form.
type optOutSite struct {
	*httptest.Server
	mu        sync.Mutex
	submitted map[string]string
}

func newOptOutSite(t *testing.T, page string) *optOutSite {
	t.Helper()
	s := &optOutSite{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			_ = r.ParseForm()
			s.mu.Lock()
			s.submitted = map[string]string{}
			for k := range r.PostForm {
				s.submitted[k] = r.PostForm.Get(k)
			}
			s.mu.Unlock()
			_, _ = w.Write([]byte("<html><body>Request received</body></html>"))
			return
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(page))
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *optOutSite) form() map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.submitted
}

const optOutForm = `<html><body><form method="post" action="/">
<input name="email"><input name="first_name"><input name="last_name">
<select name="state"><option value="">-</option><option value="CA">California</option></select>
<input type="text" id="zz" placeholder="Your city">
<button type="submit">Submit</button></form></body></html>`

// chromeBrowser is requireChrome with the full profile and the test
// server's host allowed.
func chromeBrowser(t *testing.T, cfg BrowserConfig) *Browser {
	t.Helper()
	requireChrome(t).Close()
	p := &config.Profile{FirstName: "Test", LastName: "User", Email: "test@example.com", State: "CA", City: "Sacramento"}
	b, err := New(cfg, p, []string{"127.0.0.1"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(b.Close)
	return b
}

func chromeConfig(t *testing.T) BrowserConfig {
	cfg := DefaultConfig()
	cfg.Timeout = 30 * time.Second
	cfg.ScreenshotDir = t.TempDir()
	return cfg
}

func screenshots(t *testing.T, dir string) []string {
	t.Helper()
	entries, _ := os.ReadDir(dir)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func TestNavigateAndFillSubmitsForm(t *testing.T) {
	cfg := chromeConfig(t)
	b := chromeBrowser(t, cfg)
	site := newOptOutSite(t, optOutForm)

	res, err := b.NavigateAndFill(site.URL, "acme", true)
	if err != nil {
		t.Fatalf("NavigateAndFill: %v (%+v)", err, res)
	}
	if !res.Success || !res.SubmitAttempted || res.CaptchaFound {
		t.Fatalf("result = %+v", res)
	}
	got := site.form()
	if got["email"] != "test@example.com" || got["first_name"] != "Test" || got["last_name"] != "User" || got["state"] != "CA" {
		t.Errorf("submitted form = %v", got)
	}
	if shots := screenshots(t, cfg.ScreenshotDir); len(shots) != 2 {
		t.Errorf("screenshots = %v, want filled + submitted", shots)
	}
}

func TestNavigateAndFillWithoutSubmit(t *testing.T) {
	b := chromeBrowser(t, chromeConfig(t))
	site := newOptOutSite(t, optOutForm)
	res, err := b.NavigateAndFill(site.URL, "acme", false)
	if err != nil || !res.Success || res.SubmitAttempted || len(res.FieldsFilled) == 0 {
		t.Errorf("result = %+v, %v", res, err)
	}
	if site.form() != nil {
		t.Error("form submitted without autoSubmit")
	}
}

func TestNavigateAndFillNoForm(t *testing.T) {
	b := chromeBrowser(t, chromeConfig(t))
	site := newOptOutSite(t, `<html><body><p>Email privacy@acme.example to opt out.</p></body></html>`)
	res, err := b.NavigateAndFill(site.URL, "acme", true)
	if err != nil || res.Success || res.SubmitAttempted {
		t.Errorf("result = %+v, %v", res, err)
	}
}

func TestNavigateAndFillBlockingCaptcha(t *testing.T) {
	gate := `<html><head><title>Just a moment...</title></head><body>Checking your browser</body></html>`

	cfg := chromeConfig(t)
	b := chromeBrowser(t, cfg)
	res, err := b.NavigateAndFill(newOptOutSite(t, gate).URL, "acme", true)
	if err != nil || !res.CaptchaFound || !strings.Contains(res.ErrorMessage, "Blocking CAPTCHA") {
		t.Errorf("result = %+v, %v", res, err)
	}
	if len(screenshots(t, cfg.ScreenshotDir)) != 1 {
		t.Error("no screenshot of the CAPTCHA gate")
	}

	cfg = chromeConfig(t)
	cfg.WaitForUser = true
	cfg.WaitCallback = func() error { return errors.New("user pressed q") }
	b = chromeBrowser(t, cfg)
	if res, err := b.NavigateAndFill(newOptOutSite(t, gate).URL, "acme", true); err == nil || !strings.Contains(res.ErrorMessage, "user cancelled") {
		t.Errorf("cancelled wait: %+v, %v", res, err)
	}
}

const captchaForm = `<html><body><form method="post" action="/">
<input name="email"><div class="g-recaptcha" data-sitekey="x"></div>
<button type="submit">Submit</button></form></body></html>`

func TestNavigateAndFillFormCaptcha(t *testing.T) {
	b := chromeBrowser(t, chromeConfig(t))
	res, err := b.NavigateAndFill(newOptOutSite(t, captchaForm).URL, "acme", true)
	if err != nil || !res.CaptchaFound || res.SubmitAttempted || !strings.Contains(res.ErrorMessage, "form filled") {
		t.Errorf("result = %+v, %v", res, err)
	}

	// With --wait, the user solves it and the form is submitted.
	cfg := chromeConfig(t)
	cfg.WaitForUser = true
	solved := false
	cfg.WaitCallback = func() error { solved = true; return nil }
	b = chromeBrowser(t, cfg)
	site := newOptOutSite(t, captchaForm)
	res, err = b.NavigateAndFill(site.URL, "acme", true)
	if err != nil || !solved || !res.Success || !res.SubmitAttempted {
		t.Errorf("after solving: %+v, %v", res, err)
	}
	if site.form()["email"] != "test@example.com" {
		t.Errorf("submitted form = %v", site.form())
	}

	cfg.WaitCallback = func() error { return errors.New("gave up") }
	b = chromeBrowser(t, cfg)
	if res, err := b.NavigateAndFill(newOptOutSite(t, captchaForm).URL, "acme", true); err == nil || !strings.Contains(res.ErrorMessage, "user cancelled") {
		t.Errorf("cancelled form captcha: %+v, %v", res, err)
	}
}

func TestNavigateAndFillUnreachable(t *testing.T) {
	cfg := chromeConfig(t)
	cfg.Timeout = 10 * time.Second
	b := chromeBrowser(t, cfg)
	if res, err := b.NavigateAndFill("http://127.0.0.1:1/optout", "acme", true); err == nil || res.ErrorMessage == "" {
		t.Errorf("unreachable: %+v, %v", res, err)
	}
	if res, err := b.NavigateAndFill("http://[::1", "acme", true); err == nil || !strings.Contains(res.ErrorMessage, "invalid URL") {
		t.Errorf("bad URL: %+v, %v", res, err)
	}
}
