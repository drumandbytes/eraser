package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/drumandbytes/eraser/internal/broker"
	"github.com/drumandbytes/eraser/internal/config"
	emaTemplate "github.com/drumandbytes/eraser/internal/template"
)

// unwritableConfigPath is a path nothing can be saved to on any OS: a
// directory can't be created under a regular file.
func unwritableConfigPath(t *testing.T) string {
	t.Helper()
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(blocker, "sub", "config.yaml")
}

// Every handler that saves the config must report a failed save, not pretend.
func TestHandlersReportConfigSaveFailures(t *testing.T) {
	cases := []struct {
		name, method, target string
		form                 url.Values
		want                 string
	}{
		{"new profile", http.MethodPost, "/settings/profiles/new",
			url.Values{"first_name": {"Anna"}, "last_name": {"P"}, "email": {"anna@example.com"}}, "Failed to save configuration"},
		{"edit profile", http.MethodPost, "/settings/profiles/spouse/edit",
			url.Values{"first_name": {"Anna"}, "last_name": {"P"}, "email": {"anna@example.com"}}, "Failed to save configuration"},
		{"delete profile", http.MethodPost, "/settings/profiles/spouse/delete", nil, "Failed to save configuration"},
		{"exclude broker", http.MethodPost, "/api/brokers/spokeo/exclude", nil, "Failed to save configuration"},
		{"inbox settings", http.MethodPost, "/settings/inbox",
			url.Values{"mail_provider": {"fastmail"}, "mail_address": {"a@b.co"}, "mail_username": {"a@b.co"}, "mail_password": {"pw"}}, "Failed to save configuration"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := smokeServer(t)
			s.configPath = unwritableConfigPath(t)
			rec := post(t, s, c.target, c.form)
			if !strings.Contains(rec.Body.String(), c.want) {
				t.Errorf("%s = %d %.200s", c.target, rec.Code, rec.Body.String())
			}
		})
	}
}

func TestSetupCompleteReportsSaveFailure(t *testing.T) {
	s := newTestServer(t, testConfig())
	s.configPath = unwritableConfigPath(t)
	id, err := s.sessions.Create()
	if err != nil {
		t.Fatal(err)
	}
	s.sessions.Update(id, func(sess *Session) {
		sess.Profile = config.Profile{FirstName: "Jane", LastName: "Doe", Email: "jane@example.com"}
		sess.ManualSend = true
		sess.Step = "complete"
	})
	req := httptest.NewRequest(http.MethodGet, "/setup/complete", nil)
	req.AddCookie(&http.Cookie{Name: "eraser_session", Value: id})
	rec := httptest.NewRecorder()
	s.handleSetupComplete(rec, req)
	if !strings.Contains(rec.Body.String(), "Setup - Error") && !strings.Contains(rec.Body.String(), "failed") {
		t.Errorf("setup should surface the failed save: %.300s", rec.Body.String())
	}
}

func TestNewServerNotesAMissingConfigFile(t *testing.T) {
	eng, err := emaTemplate.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "config.yaml")
	s, err := NewServer(0, testConfig(), path, &broker.BrokerDatabase{}, nil, eng)
	if err != nil {
		t.Fatal(err)
	}
	if s.cfgStamp != "none" {
		t.Errorf("a config path with no file yet should start as %q, got %q", "none", s.cfgStamp)
	}
}

func TestBrokerListHonoursConfiguredRegions(t *testing.T) {
	cfg := testConfig("default")
	cfg.Options.Regions = []string{"eu"}
	s := newTestServer(t, cfg)
	s.brokers().Brokers = []broker.Broker{
		{ID: "us-one", Name: "US One", Region: "us"},
		{ID: "eu-one", Name: "EU One", Region: "eu"},
		{ID: "global-one", Name: "Global One", Region: "global"},
	}
	got := s.getBrokersWithStatus("default", "", "", "", "", nil, nil, false, false)
	if len(got) != 2 || got[0].ID == "us-one" || got[1].ID == "us-one" {
		t.Errorf("regions: [eu] should keep eu and global only, got %+v", got)
	}
}
