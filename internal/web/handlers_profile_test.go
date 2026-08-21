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

func TestSlugifyProfileID(t *testing.T) {
	tests := []struct {
		name      string
		first     string
		last      string
		existing  []config.NamedProfile
		want      string
	}{
		{"basic", "Jane", "Doe", nil, "jane-doe"},
		{"diacritics and case", "Māris", "Popēns", nil, "m-ris-pop-ns"},
		{"collision appends -2", "Jane", "Doe", []config.NamedProfile{{ID: "jane-doe"}}, "jane-doe-2"},
		{"collision is case-insensitive", "Jane", "Doe", []config.NamedProfile{{ID: "JANE-DOE"}}, "jane-doe-2"},
		{"multiple collisions increment", "Jane", "Doe", []config.NamedProfile{{ID: "jane-doe"}, {ID: "jane-doe-2"}}, "jane-doe-3"},
		{"empty name falls back", "", "", nil, "profile"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := slugifyProfileID(tt.first, tt.last, tt.existing)
			if got != tt.want {
				t.Errorf("slugifyProfileID(%q, %q, %v) = %q, want %q", tt.first, tt.last, tt.existing, got, tt.want)
			}
		})
	}
}

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
		"first_name": {"Anna"},
		"last_name":  {"Popena"},
		"email":      {"anna@example.com"},
		"city":       {"Riga"},
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
	if profiles[1].Email != "anna@example.com" || profiles[1].City != "Riga" {
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
