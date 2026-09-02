package web

import (
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/drumandbytes/eraser/internal/broker"
	"github.com/drumandbytes/eraser/internal/config"
)

// wizardClient drives the setup wizard through the real router (CSRF,
// middleware, cookies and all) the way a browser would.
type wizardClient struct {
	t    *testing.T
	srv  *httptest.Server
	http *http.Client
}

func newWizardClient(t *testing.T) (*wizardClient, *Server) {
	t.Helper()

	s := newTestServer(t, nil) // no config yet - fresh install
	s.configPath = filepath.Join(t.TempDir(), "config.yaml")
	s.brokerDB.Brokers = []broker.Broker{{ID: "spokeo", Name: "Spokeo", Region: "us"}}

	ts := httptest.NewServer(s.setupRouter())
	t.Cleanup(ts.Close)

	jar, _ := cookiejar.New(nil)
	return &wizardClient{t: t, srv: ts, http: &http.Client{
		Jar: jar,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse // inspect each hop
		},
	}}, s
}

// csrfToken fetches a page and extracts the hidden gorilla.csrf.Token input.
func (c *wizardClient) csrfToken(path string) string {
	c.t.Helper()
	resp, err := c.http.Get(c.srv.URL + path)
	if err != nil {
		c.t.Fatalf("GET %s: %v", path, err)
	}
	body := readBody(c.t, resp)
	const marker = `name="gorilla.csrf.Token" value="`
	i := strings.Index(body, marker)
	if i < 0 {
		c.t.Fatalf("no CSRF token field on %s\n%s", path, body)
	}
	rest := body[i+len(marker):]
	return rest[:strings.IndexByte(rest, '"')]
}

func (c *wizardClient) post(path string, form url.Values) *http.Response {
	c.t.Helper()
	req, _ := http.NewRequest(http.MethodPost, c.srv.URL+path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Referer", c.srv.URL+path) // gorilla/csrf requires a same-origin referer
	resp, err := c.http.Do(req)
	if err != nil {
		c.t.Fatalf("POST %s: %v", path, err)
	}
	return resp
}

// TestSetupWizardManualPath walks a fresh install through the wizard choosing
// "I'll send the emails myself", and asserts a valid manual-mode config lands
// on disk. This is the whole first-run experience for a privacy-maximalist
// user - it must not dead-end.
func TestSetupWizardManualPath(t *testing.T) {
	c, s := newWizardClient(t)

	// Step 1: profile.
	form := url.Values{
		"gorilla.csrf.Token": {c.csrfToken("/setup/profile")},
		"first_name":         {"Ada"},
		"last_name":          {"Lovelace"},
		"email":              {"ada@example.com"},
		"country":            {"United Kingdom"},
	}
	resp := c.post("/setup/profile", form)
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("profile POST: got %d, want 302\n%s", resp.StatusCode, body)
	}
	if loc := resp.Header.Get("Location"); loc != "/setup/email" {
		t.Fatalf("profile POST redirected to %q, want /setup/email (the profile did not persist to the session)", loc)
	}

	// Step 2: choose manual.
	form = url.Values{
		"gorilla.csrf.Token": {c.csrfToken("/setup/email")},
		"manual":             {"1"},
	}
	resp = c.post("/setup/email", form)
	if resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != "/setup/complete" {
		t.Fatalf("email POST (manual): got %d -> %q, want 302 -> /setup/complete", resp.StatusCode, resp.Header.Get("Location"))
	}

	// Step 3: complete - writes the config.
	resp, err := c.http.Get(c.srv.URL + "/setup/complete")
	if err != nil {
		t.Fatal(err)
	}
	if b := readBody(t, resp); resp.StatusCode != http.StatusOK || strings.Contains(b, "Error") {
		t.Fatalf("complete: got %d\n%s", resp.StatusCode, b)
	}

	saved, err := config.Load(s.configPath)
	if err != nil {
		t.Fatalf("config not written: %v", err)
	}
	if saved.Profile.FirstName != "Ada" || saved.Profile.Email != "ada@example.com" {
		t.Errorf("saved profile wrong: %+v", saved.Profile)
	}
	if !saved.IsManualSend() {
		t.Errorf("expected manual send mode, got %q", saved.Options.SendMode)
	}
	if saved.Options.Template != "gdpr" {
		t.Errorf("expected gdpr template default, got %q", saved.Options.Template)
	}
	if err := saved.Validate(); err != nil {
		t.Errorf("saved manual config does not validate: %v", err)
	}
}

// TestSetupWizardProfileValidation: a missing required field re-renders the
// form with an error, does not redirect, and does not create a half-populated
// session.
func TestSetupWizardProfileValidation(t *testing.T) {
	c, _ := newWizardClient(t)

	form := url.Values{
		"gorilla.csrf.Token": {c.csrfToken("/setup/profile")},
		"first_name":         {"Ada"},
		// no last_name, no email
	}
	resp := c.post("/setup/profile", form)
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("got %d, want 200 (form re-render)", resp.StatusCode)
	}
	if !strings.Contains(body, "Last name is required") || !strings.Contains(body, "Email is required") {
		t.Errorf("expected validation errors in body\n%s", body)
	}
}

func readBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return string(b)
}
