package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

// withURLParam attaches a chi URL param to a request the way the router
// would after matching a route like "/settings/profiles/{profileID}/edit" -
// needed here since these tests call the handler directly rather than
// through the full router.
func withURLParam(r *http.Request, key, value string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(key, value)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

// config.SlugifyProfileID's own behavior (basic slugify, collisions,
// diacritics) is covered by internal/config's tests now that the web
// package no longer has its own copy of that logic - see
// internal/config/config_test.go.

func TestHandleSettingsProfileNewCreatesProfile(t *testing.T) {
	s := newTestServer(t, testConfig())
	s.configPath = filepath.Join(t.TempDir(), "config.yaml")

	// GET should render without error (regression check: this panicked
	// with "index of untyped nil" before the handler set an Errors key
	// on the no-error GET path, since the template does {{index .Errors "_"}}).
	getReq := httptest.NewRequest(http.MethodGet, "/settings/profiles/new", nil)
	getRec := httptest.NewRecorder()
	s.handleSettingsProfileNew(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("GET: expected 200, got %d: %s", getRec.Code, getRec.Body.String())
	}

	form := url.Values{
		"first_name":  {"Anna"},
		"middle_name": {"Marija"},
		"last_name":   {"Popena"},
		"email":       {"anna@example.com"},
		"city":        {"Riga"},
	}
	postReq := httptest.NewRequest(http.MethodPost, "/settings/profiles/new", strings.NewReader(form.Encode()))
	postReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	postRec := httptest.NewRecorder()
	s.handleSettingsProfileNew(postRec, postReq)

	if postRec.Code != http.StatusSeeOther {
		t.Fatalf("POST: expected 303 redirect, got %d: %s", postRec.Code, postRec.Body.String())
	}
	if loc := postRec.Header().Get("Location"); loc != "/settings" {
		t.Errorf("expected redirect to /settings, got %q", loc)
	}

	profiles := s.getConfig().GetProfiles()
	if len(profiles) != 2 {
		t.Fatalf("expected 2 profiles (seeded default + new), got %d: %+v", len(profiles), profiles)
	}
	if profiles[0].ID != "default" {
		t.Errorf("expected legacy profile to be seeded as %q, got %q", "default", profiles[0].ID)
	}
	if profiles[1].ID != "anna-popena" {
		t.Errorf("expected new profile ID %q, got %q", "anna-popena", profiles[1].ID)
	}
	if profiles[1].Email != "anna@example.com" || profiles[1].City != "Riga" || profiles[1].MiddleName != "Marija" {
		t.Errorf("new profile fields not saved correctly: %+v", profiles[1])
	}
}

func TestHandleSettingsProfileNewValidatesRequiredFields(t *testing.T) {
	s := newTestServer(t, testConfig())
	s.configPath = filepath.Join(t.TempDir(), "config.yaml")

	form := url.Values{"first_name": {"Anna"}} // missing last_name and email
	req := httptest.NewRequest(http.MethodPost, "/settings/profiles/new", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	s.handleSettingsProfileNew(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 (re-render with errors), got %d", rec.Code)
	}
	if len(s.getConfig().GetProfiles()) != 1 {
		t.Error("no profile should have been added when validation fails")
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Last name is required") || !strings.Contains(body, "Email is required") {
		t.Errorf("expected validation error messages in response body, got: %s", body)
	}
}

func TestHandleSettingsProfileEditUpdatesFields(t *testing.T) {
	s := newTestServer(t, testConfig("default", "spouse"))
	s.configPath = filepath.Join(t.TempDir(), "config.yaml")

	// GET should pre-fill the form from the existing profile, not a blank one.
	getReq := withURLParam(httptest.NewRequest(http.MethodGet, "/settings/profiles/spouse/edit", nil), "profileID", "spouse")
	getRec := httptest.NewRecorder()
	s.handleSettingsProfileEdit(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("GET: expected 200, got %d: %s", getRec.Code, getRec.Body.String())
	}
	if !strings.Contains(getRec.Body.String(), "spouse@example.com") {
		t.Errorf("expected GET form to be pre-filled with the existing profile's email, body: %s", getRec.Body.String())
	}

	form := url.Values{
		"first_name": {"Updated"},
		"last_name":  {"Name"},
		"email":      {"updated@example.com"},
		"city":       {"Vilnius"},
	}
	postReq := withURLParam(httptest.NewRequest(http.MethodPost, "/settings/profiles/spouse/edit", strings.NewReader(form.Encode())), "profileID", "spouse")
	postReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	postRec := httptest.NewRecorder()
	s.handleSettingsProfileEdit(postRec, postReq)

	if postRec.Code != http.StatusSeeOther {
		t.Fatalf("POST: expected 303 redirect, got %d: %s", postRec.Code, postRec.Body.String())
	}

	profiles := s.getConfig().GetProfiles()
	if len(profiles) != 2 {
		t.Fatalf("expected profile count to stay 2, got %d: %+v", len(profiles), profiles)
	}
	if profiles[0].ID != "default" {
		t.Errorf("expected first profile to remain %q untouched, got %+v", "default", profiles[0])
	}
	if profiles[1].ID != "spouse" {
		t.Errorf("expected edited profile's ID to stay %q, got %q", "spouse", profiles[1].ID)
	}
	if profiles[1].FirstName != "Updated" || profiles[1].Email != "updated@example.com" || profiles[1].City != "Vilnius" {
		t.Errorf("edited profile fields not saved correctly: %+v", profiles[1])
	}
}

func TestHandleSettingsProfileEditUnknownIDReturns404(t *testing.T) {
	s := newTestServer(t, testConfig("default"))
	s.configPath = filepath.Join(t.TempDir(), "config.yaml")

	req := withURLParam(httptest.NewRequest(http.MethodGet, "/settings/profiles/nonexistent/edit", nil), "profileID", "nonexistent")
	rec := httptest.NewRecorder()
	s.handleSettingsProfileEdit(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 for unknown profile ID, got %d", rec.Code)
	}
}

func TestHandleSettingsProfileEditLegacySingleProfileWritesBackToProfileBlock(t *testing.T) {
	// No profiles: list configured - just the legacy top-level profile:
	// block, synthesized as the "default" profile by GetProfiles().
	s := newTestServer(t, testConfig())
	s.configPath = filepath.Join(t.TempDir(), "config.yaml")

	form := url.Values{
		"first_name": {"New"},
		"last_name":  {"Name"},
		"email":      {"new@example.com"},
	}
	req := withURLParam(httptest.NewRequest(http.MethodPost, "/settings/profiles/default/edit", strings.NewReader(form.Encode())), "profileID", "default")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	s.handleSettingsProfileEdit(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected 303 redirect, got %d: %s", rec.Code, rec.Body.String())
	}

	cfg := s.getConfig()
	if len(cfg.Profiles) != 0 {
		t.Errorf("expected editing the legacy default profile to stay in single-profile mode (no profiles: list), got %+v", cfg.Profiles)
	}
	if cfg.Profile.FirstName != "New" || cfg.Profile.Email != "new@example.com" {
		t.Errorf("expected legacy profile: block to be updated, got %+v", cfg.Profile)
	}
}

func TestHandleSettingsProfileDeleteRemovesProfile(t *testing.T) {
	s := newTestServer(t, testConfig("default", "spouse"))
	s.configPath = filepath.Join(t.TempDir(), "config.yaml")

	req := withURLParam(httptest.NewRequest(http.MethodPost, "/settings/profiles/spouse/delete", nil), "profileID", "spouse")
	rec := httptest.NewRecorder()
	s.handleSettingsProfileDelete(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected 303 redirect, got %d: %s", rec.Code, rec.Body.String())
	}

	profiles := s.getConfig().GetProfiles()
	if len(profiles) != 1 || profiles[0].ID != "default" {
		t.Errorf("expected only %q to remain, got %+v", "default", profiles)
	}
}

func TestHandleSettingsProfileDeleteRefusesToRemoveLastProfile(t *testing.T) {
	s := newTestServer(t, testConfig("default"))
	s.configPath = filepath.Join(t.TempDir(), "config.yaml")

	req := withURLParam(httptest.NewRequest(http.MethodPost, "/settings/profiles/default/delete", nil), "profileID", "default")
	rec := httptest.NewRecorder()
	s.handleSettingsProfileDelete(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 when deleting the only profile, got %d", rec.Code)
	}
	if len(s.getConfig().GetProfiles()) != 1 {
		t.Error("the only profile should not have been removed")
	}
}

func TestHandleSettingsProfileDeleteClearsActiveProfileCookie(t *testing.T) {
	s := newTestServer(t, testConfig("default", "spouse"))
	s.configPath = filepath.Join(t.TempDir(), "config.yaml")

	req := withURLParam(httptest.NewRequest(http.MethodPost, "/settings/profiles/spouse/delete", nil), "profileID", "spouse")
	req.AddCookie(&http.Cookie{Name: activeProfileCookie, Value: "spouse"})
	rec := httptest.NewRecorder()
	s.handleSettingsProfileDelete(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected 303 redirect, got %d: %s", rec.Code, rec.Body.String())
	}

	var cleared bool
	for _, c := range rec.Result().Cookies() {
		if c.Name == activeProfileCookie && c.MaxAge < 0 {
			cleared = true
		}
	}
	if !cleared {
		t.Error("expected the active-profile cookie to be cleared after deleting the profile it pointed to")
	}
}
