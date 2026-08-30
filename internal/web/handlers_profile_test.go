package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eraser-privacy/eraser/internal/config"
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

// profileWithExtras returns a config whose sole profile carries every field
// the web profile form does *not* render an input for. These are reachable
// only from `eraser init` / `eraser profile edit` on the CLI, which makes
// them exactly the fields a web-side edit is liable to quietly drop.
func profileWithExtras() *config.Config {
	return &config.Config{
		Profiles: []config.NamedProfile{{
			ID: "spouse",
			Profile: config.Profile{
				FirstName:         "Test",
				LastName:          "Spouse",
				Email:             "spouse@example.com",
				AdditionalEmails:  []string{"old@example.com", "work@company.com"},
				NameVariants:      []string{"Maris"},
				PreviousAddresses: []string{"1 Old St, Riga"},
				AdditionalPhones:  []string{"+371 20000000"},
				DateOfBirth:       "1990-01-01",
			},
		}},
	}
}

func postProfileEdit(t *testing.T, s *Server, id string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	req := withURLParam(
		httptest.NewRequest(http.MethodPost, "/settings/profiles/"+id+"/edit", strings.NewReader(form.Encode())),
		"profileID", id)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	s.handleSettingsProfileEdit(rec, req)
	return rec
}

// TestHandleSettingsProfileEditPreservesCLIOnlyFields is the regression test
// for the silent wipe: buildProfileFromForm used to construct a fresh
// config.Profile from the ten form inputs, and both write paths assign that
// over the whole stored struct. Editing your city in the web UI therefore
// erased additional emails, name variants, previous addresses, additional
// phones and date of birth - weakening every later removal request with no
// visible sign that anything had been lost.
func TestHandleSettingsProfileEditPreservesCLIOnlyFields(t *testing.T) {
	s := newTestServer(t, profileWithExtras())
	s.configPath = filepath.Join(t.TempDir(), "config.yaml")

	// A submission from a form that has no input for any of the extras. The
	// additional_emails key is absent entirely, standing in for any page that
	// edits a profile without rendering that textarea.
	rec := postProfileEdit(t, s, "spouse", url.Values{
		"first_name": {"Test"},
		"last_name":  {"Spouse"},
		"email":      {"spouse@example.com"},
		"city":       {"Vilnius"},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d: %s", rec.Code, rec.Body.String())
	}

	got := s.getConfig().GetProfiles()[0].Profile
	if got.City != "Vilnius" {
		t.Errorf("the edited field should still be applied, got City=%q", got.City)
	}
	if len(got.AdditionalEmails) != 2 || got.AdditionalEmails[0] != "old@example.com" {
		t.Errorf("AdditionalEmails wiped by edit: %+v", got.AdditionalEmails)
	}
	if len(got.NameVariants) != 1 || got.NameVariants[0] != "Maris" {
		t.Errorf("NameVariants wiped by edit: %+v", got.NameVariants)
	}
	if len(got.PreviousAddresses) != 1 {
		t.Errorf("PreviousAddresses wiped by edit: %+v", got.PreviousAddresses)
	}
	if len(got.AdditionalPhones) != 1 {
		t.Errorf("AdditionalPhones wiped by edit: %+v", got.AdditionalPhones)
	}
	if got.DateOfBirth != "1990-01-01" {
		t.Errorf("DateOfBirth wiped by edit: %q", got.DateOfBirth)
	}
}

func TestHandleSettingsProfileEditSavesAdditionalEmails(t *testing.T) {
	s := newTestServer(t, testConfig("spouse"))
	s.configPath = filepath.Join(t.TempDir(), "config.yaml")

	// Newlines are the documented format; a comma-separated line is accepted
	// too, since that's what the CLI prompt asks for and what a user pasting
	// from it will type.
	rec := postProfileEdit(t, s, "spouse", url.Values{
		"first_name":        {"Test"},
		"last_name":         {"Spouse"},
		"email":             {"spouse@example.com"},
		"additional_emails": {"  old@example.com \n\n work@company.com, third@example.com \n"},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d: %s", rec.Code, rec.Body.String())
	}

	got := s.getConfig().GetProfiles()[0].AdditionalEmails
	want := []string{"old@example.com", "work@company.com", "third@example.com"}
	if len(got) != len(want) {
		t.Fatalf("got %d addresses %+v, want %d %+v", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("address %d: got %q, want %q", i, got[i], want[i])
		}
	}
}

// An empty textarea has to actually clear the list. This is the case that
// breaks if preservation is keyed off "is the parsed value empty" rather than
// "was the input present in the submission" - the old values would resurrect
// on every save and the field could never be emptied from the UI.
func TestHandleSettingsProfileEditClearsAdditionalEmails(t *testing.T) {
	s := newTestServer(t, profileWithExtras())
	s.configPath = filepath.Join(t.TempDir(), "config.yaml")

	rec := postProfileEdit(t, s, "spouse", url.Values{
		"first_name":        {"Test"},
		"last_name":         {"Spouse"},
		"email":             {"spouse@example.com"},
		"additional_emails": {"   \n  "},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d: %s", rec.Code, rec.Body.String())
	}

	got := s.getConfig().GetProfiles()[0].Profile
	if len(got.AdditionalEmails) != 0 {
		t.Errorf("submitting an empty textarea should clear the list, got %+v", got.AdditionalEmails)
	}
	// Clearing the field the form owns must not disturb the ones it doesn't.
	if len(got.NameVariants) != 1 {
		t.Errorf("clearing emails should not touch NameVariants, got %+v", got.NameVariants)
	}
}

func TestHandleSettingsProfileEditDedupesAndDropsPrimaryEmail(t *testing.T) {
	s := newTestServer(t, testConfig("spouse"))
	s.configPath = filepath.Join(t.TempDir(), "config.yaml")

	rec := postProfileEdit(t, s, "spouse", url.Values{
		"first_name":        {"Test"},
		"last_name":         {"Spouse"},
		"email":             {"spouse@example.com"},
		"additional_emails": {"old@example.com\nSPOUSE@example.com\nOld@Example.com\nkeep@example.com"},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d: %s", rec.Code, rec.Body.String())
	}

	got := s.getConfig().GetProfiles()[0].AdditionalEmails
	want := []string{"old@example.com", "keep@example.com"}
	if len(got) != len(want) {
		t.Fatalf("got %+v, want %+v (primary and case-insensitive dupes dropped)", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("address %d: got %q, want %q", i, got[i], want[i])
		}
	}
}

func TestHandleSettingsProfileEditRejectsInvalidAdditionalEmail(t *testing.T) {
	s := newTestServer(t, profileWithExtras())
	s.configPath = filepath.Join(t.TempDir(), "config.yaml")

	rec := postProfileEdit(t, s, "spouse", url.Values{
		"first_name":        {"Test"},
		"last_name":         {"Spouse"},
		"email":             {"spouse@example.com"},
		"additional_emails": {"good@example.com\nnot-an-email"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 re-render with errors, got %d", rec.Code)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "not-an-email") {
		t.Error("the offending entry should be echoed back in the textarea so the user can fix it")
	}
	if !strings.Contains(body, "Not a valid email address") {
		t.Errorf("expected a field error message, body: %s", body)
	}
	// Nothing may be persisted on a failed validation.
	if got := s.getConfig().GetProfiles()[0].AdditionalEmails; len(got) != 2 || got[0] != "old@example.com" {
		t.Errorf("stored addresses should be untouched on validation failure, got %+v", got)
	}
}
