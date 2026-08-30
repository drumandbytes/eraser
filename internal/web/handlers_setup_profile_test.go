package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eraser-privacy/eraser/internal/config"
)

// setupProfileGET drives the wizard's profile step and returns the rendered
// page plus a cookie for a session to carry into the follow-up POST. The GET
// handler doesn't establish a session itself (only the POST does, via
// getOrCreateSession), so the test creates one up front the way a browser
// would have one by the time it reaches this step.
func setupProfileGET(t *testing.T, s *Server) (*httptest.ResponseRecorder, *http.Cookie) {
	t.Helper()
	id, err := s.sessions.Create()
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	cookie := &http.Cookie{Name: "eraser_session", Value: id}

	req := httptest.NewRequest(http.MethodGet, "/setup/profile", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	s.handleSetupProfile(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /setup/profile: expected 200, got %d", rec.Code)
	}
	return rec, cookie
}

// TestSetupWizardRerunPreservesCLIOnlyProfileFields covers the wizard half of
// the same wipe. /setup has no "already configured" guard (every route is
// registered unconditionally), and handleSetupComplete assigns
// cfg.Profile = session.Profile wholesale - so walking back through the
// wizard on a configured install used to erase the CLI-only profile fields.
// A config-level rescue was already in place there for Profiles/Inbox/
// Pipeline/Options; this is the same fix one level down, on Profile itself.
func TestSetupWizardRerunPreservesCLIOnlyProfileFields(t *testing.T) {
	cfg := &config.Config{
		Profile: config.Profile{
			FirstName:         "Test",
			LastName:          "User",
			Email:             "test@example.com",
			AdditionalEmails:  []string{"old@example.com"},
			NameVariants:      []string{"Maris"},
			PreviousAddresses: []string{"1 Old St, Riga"},
			AdditionalPhones:  []string{"+371 20000000"},
			DateOfBirth:       "1990-01-01",
		},
	}
	s := newTestServer(t, cfg)
	s.configPath = filepath.Join(t.TempDir(), "config.yaml")

	// The GET must pre-fill from the saved config rather than the (empty)
	// fresh session. This is load-bearing, not cosmetic: the additional-
	// emails textarea is always submitted, so rendering it blank here would
	// make the subsequent POST read as "the user cleared this".
	getRec, cookie := setupProfileGET(t, s)
	if cookie == nil {
		t.Fatal("expected the wizard to set a session cookie")
	}
	if !strings.Contains(getRec.Body.String(), "old@example.com") {
		t.Error("GET should pre-fill the additional-emails textarea from the saved config")
	}

	form := url.Values{
		"first_name":        {"Test"},
		"last_name":         {"User"},
		"email":             {"test@example.com"},
		"city":              {"Riga"},
		"additional_emails": {"old@example.com"},
	}
	postReq := httptest.NewRequest(http.MethodPost, "/setup/profile", strings.NewReader(form.Encode()))
	postReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	postReq.AddCookie(cookie)
	postRec := httptest.NewRecorder()
	s.handleSetupProfile(postRec, postReq)

	if postRec.Code != http.StatusFound {
		t.Fatalf("POST /setup/profile: expected 302, got %d: %s", postRec.Code, postRec.Body.String())
	}

	session := s.sessions.Get(cookie.Value)
	if session == nil {
		t.Fatal("expected the session to survive the POST")
	}
	got := session.Profile
	if got.City != "Riga" {
		t.Errorf("edited field not applied, got City=%q", got.City)
	}
	if len(got.AdditionalEmails) != 1 || got.AdditionalEmails[0] != "old@example.com" {
		t.Errorf("AdditionalEmails not carried into the wizard session: %+v", got.AdditionalEmails)
	}
	if len(got.NameVariants) != 1 {
		t.Errorf("NameVariants dropped by the wizard: %+v", got.NameVariants)
	}
	if len(got.PreviousAddresses) != 1 {
		t.Errorf("PreviousAddresses dropped by the wizard: %+v", got.PreviousAddresses)
	}
	if len(got.AdditionalPhones) != 1 {
		t.Errorf("AdditionalPhones dropped by the wizard: %+v", got.AdditionalPhones)
	}
	// DateOfBirth has no input in the shared profile partial. The wizard used
	// to overwrite it from a non-existent "dob" field on every submit, which
	// silently reset it to "" - it's now only read when actually submitted.
	if got.DateOfBirth != "1990-01-01" {
		t.Errorf("DateOfBirth reset by the wizard's absent dob field: %q", got.DateOfBirth)
	}
}

// A first-time run has no saved config to fall back on; the wizard must still
// render and accept a profile.
func TestSetupWizardFreshInstallHasNoPrefill(t *testing.T) {
	s := newTestServer(t, &config.Config{})
	s.configPath = filepath.Join(t.TempDir(), "config.yaml")

	getRec, cookie := setupProfileGET(t, s)
	if cookie == nil {
		t.Fatal("expected the wizard to set a session cookie")
	}
	if strings.Contains(getRec.Body.String(), "old@example.com") {
		t.Error("nothing should be pre-filled on a fresh install")
	}

	form := url.Values{
		"first_name":        {"New"},
		"last_name":         {"User"},
		"email":             {"new@example.com"},
		"additional_emails": {"alias@example.com"},
	}
	postReq := httptest.NewRequest(http.MethodPost, "/setup/profile", strings.NewReader(form.Encode()))
	postReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	postReq.AddCookie(cookie)
	postRec := httptest.NewRecorder()
	s.handleSetupProfile(postRec, postReq)

	if postRec.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d: %s", postRec.Code, postRec.Body.String())
	}
	got := s.sessions.Get(cookie.Value).Profile
	if len(got.AdditionalEmails) != 1 || got.AdditionalEmails[0] != "alias@example.com" {
		t.Errorf("additional emails not collected on a fresh run: %+v", got.AdditionalEmails)
	}
}
