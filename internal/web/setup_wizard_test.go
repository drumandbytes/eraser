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

// get fetches a page and returns its body (used to prime cookies / confirm a
// step renders before posting to it).
func (c *wizardClient) get(path string) string {
	c.t.Helper()
	resp, err := c.http.Get(c.srv.URL + path)
	if err != nil {
		c.t.Fatalf("GET %s: %v", path, err)
	}
	return readBody(c.t, resp)
}

// post submits a form the way a same-origin browser navigation would. The CSRF
// layer (filippo.io/csrf/gorilla) keys off Sec-Fetch-Site, so set it.
func (c *wizardClient) post(path string, form url.Values) *http.Response {
	c.t.Helper()
	req, _ := http.NewRequest(http.MethodPost, c.srv.URL+path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
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
	c.get("/setup/profile")
	form := url.Values{
		"first_name": {"Ada"},
		"last_name":  {"Lovelace"},
		"email":      {"ada@example.com"},
		"country":    {"United Kingdom"},
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
	c.get("/setup/email")
	form = url.Values{"manual": {"1"}}
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
	if p := saved.PrimaryProfile(); p.ID != config.DefaultProfileID || p.FirstName != "Ada" || p.Email != "ada@example.com" || len(saved.Profiles) != 1 {
		t.Errorf("saved profiles wrong: %+v", saved.Profiles)
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

	c.get("/setup/profile")
	form := url.Values{
		"first_name": {"Ada"},
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

// TestCSRFBlocksCrossOrigin confirms the CSRF layer is actually doing its job:
// a browser POST tagged cross-site is rejected, a same-origin one is allowed,
// and a non-browser request (no Sec-Fetch-Site, no Origin) is allowed.
func TestCSRFBlocksCrossOrigin(t *testing.T) {
	c, _ := newWizardClient(t)
	c.get("/setup/profile") // prime the session cookie

	valid := url.Values{"first_name": {"A"}, "last_name": {"B"}, "email": {"a@b.com"}}

	newReq := func(secFetchSite string) *http.Request {
		req, _ := http.NewRequest(http.MethodPost, c.srv.URL+"/setup/profile", strings.NewReader(valid.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if secFetchSite != "" {
			req.Header.Set("Sec-Fetch-Site", secFetchSite)
		}
		return req
	}

	cases := []struct {
		name          string
		secFetchSite  string
		wantForbidden bool
	}{
		{"cross-site browser POST", "cross-site", true},
		{"same-origin browser POST", "same-origin", false},
		{"non-browser POST (no fetch metadata)", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := c.http.Do(newReq(tc.secFetchSite))
			if err != nil {
				t.Fatal(err)
			}
			_ = resp.Body.Close()
			forbidden := resp.StatusCode == http.StatusForbidden
			if forbidden != tc.wantForbidden {
				t.Fatalf("got status %d (forbidden=%v), want forbidden=%v", resp.StatusCode, forbidden, tc.wantForbidden)
			}
		})
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

// Settings -> "Edit Email Settings" re-runs the wizard on an existing install.
// Finishing it must change only the primary profile and the sending account,
// not drop other profiles, the inbox or tuned options.
func TestSetupWizardRerunKeepsExistingConfig(t *testing.T) {
	c, s := newWizardClient(t)
	existing := testConfig("default", "spouse")
	existing.Inbox = config.InboxConfig{Enabled: true, Server: "imap.example.org", Port: 993, Email: "default@example.com", Password: "imap-pw"}
	existing.Options.DailySendLimit = 120
	existing.Email = config.EmailConfig{From: "default@example.com", SMTP: config.SMTPConfig{Host: "smtp.gmail.com", Port: 465, Username: "default@example.com", Password: "old-pw"}}
	s.config.Store(existing)

	if body := c.get("/setup/profile"); !strings.Contains(body, `value="default@example.com"`) {
		t.Fatal("profile step not prefilled from the saved primary profile")
	}
	c.post("/setup/profile", url.Values{"first_name": {"Ada"}, "last_name": {"Lovelace"}, "email": {"default@example.com"}})
	c.get("/setup/email")
	resp := c.post("/setup/email", url.Values{
		"mail_provider": {"fastmail"},
		"mail_address":  {"default@example.com"},
		"mail_password": {"new-pw"},
	})
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("email POST: got %d\n%s", resp.StatusCode, readBody(t, resp))
	}
	readBody(t, resp)
	c.get("/setup/complete")

	saved, err := config.Load(s.configPath)
	if err != nil {
		t.Fatalf("config not written: %v", err)
	}
	if len(saved.Profiles) != 2 || saved.PrimaryProfile().FirstName != "Ada" {
		t.Errorf("profiles = %+v, want default updated and spouse kept", saved.Profiles)
	}
	if saved.Inbox.Server != "imap.example.org" || saved.Options.DailySendLimit != 120 {
		t.Errorf("inbox/options lost: %+v %+v", saved.Inbox, saved.Options)
	}
	if saved.Email.SMTP.Host != "smtp.fastmail.com" || saved.Email.SMTP.Password != "new-pw" {
		t.Errorf("email not updated: %+v", saved.Email)
	}
}

// The wizard's profile step reuses the profile fields but has no mail-account
// data (the account is the next step): the section must not render there as
// an empty provider dropdown.
func TestSetupProfileStepHasNoEmptyMailSection(t *testing.T) {
	c, _ := newWizardClient(t)
	if body := c.get("/setup/profile"); strings.Contains(body, `name="mail_provider"`) {
		t.Error("setup profile step renders the dedicated mail-account section")
	}
	c.post("/setup/profile", url.Values{"first_name": {"Ada"}, "last_name": {"Lovelace"}, "email": {"ada@example.com"}})
	if body := c.get("/setup/email"); !strings.Contains(body, `<option value="fastmail"`) {
		t.Error("setup email step lost its provider options")
	}
}
