package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eraser-privacy/eraser/internal/broker"
	"github.com/eraser-privacy/eraser/internal/config"
	emaTemplate "github.com/eraser-privacy/eraser/internal/template"
)

func templateTestServer(t *testing.T, cfg *config.Config) *Server {
	t.Helper()
	eng, err := emaTemplate.NewEngine()
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := config.Save(path, cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}
	s, err := NewServer(0, cfg, path, &broker.BrokerDatabase{}, nil, eng)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	return s
}

func ukCfg(tmpl string) *config.Config {
	return &config.Config{
		Profile: config.Profile{FirstName: "Alex", LastName: "Fenwick", Email: "alex@example.co.uk"},
		Options: config.Options{Template: tmpl},
	}
}

// Saving the selector must change what actually gets sent: both the CLI and
// the web send path read options.template, so persisting it is the whole
// feature. A selector that updated only the page would silently keep sending
// the old template.
func TestSaveSelectedTemplatePersists(t *testing.T) {
	s := templateTestServer(t, ukCfg("gdpr"))

	form := url.Values{"template": {"uk-access"}}
	req := httptest.NewRequest(http.MethodPost, "/settings/template", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	s.handleSettingsTemplate(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := s.getConfig().Options.Template; got != "uk-access" {
		t.Errorf("in-memory config template = %q, want uk-access", got)
	}

	// And it must survive a restart, not just live in memory.
	raw, err := os.ReadFile(s.configPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(raw), "uk-access") {
		t.Errorf("config file does not contain the saved template:\n%s", raw)
	}
}

// An unknown name must be refused rather than written to config.yaml, where
// it would fail at send time once per broker with no obvious cause.
func TestSaveSelectedTemplateRejectsUnknown(t *testing.T) {
	s := templateTestServer(t, ukCfg("gdpr"))

	form := url.Values{"template": {"not-a-template"}}
	req := httptest.NewRequest(http.MethodPost, "/settings/template", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	s.handleSettingsTemplate(rec, req)

	if got := s.getConfig().Options.Template; got != "gdpr" {
		t.Errorf("template changed to %q; an unknown name must be rejected", got)
	}
	if !strings.Contains(rec.Body.String(), "Unknown template") {
		t.Error("response should explain that the template name was not recognised")
	}
}

// The preview is the point of the box - it must show the real rendered
// letter for the requested template, with the profile filled in.
func TestTemplatePreviewRendersRequestedTemplate(t *testing.T) {
	s := templateTestServer(t, ukCfg("gdpr"))

	req := httptest.NewRequest(http.MethodGet, "/api/template/preview?template=uk-access", nil)
	rec := httptest.NewRecorder()
	s.handleAPITemplatePreview(rec, req)

	body := rec.Body.String()
	for _, want := range []string{"Article 15", "Alex Fenwick", "Example Broker"} {
		if !strings.Contains(body, want) {
			t.Errorf("preview missing %q", want)
		}
	}
	// Previewing must not change what is configured.
	if got := s.getConfig().Options.Template; got != "gdpr" {
		t.Errorf("preview changed the saved template to %q", got)
	}
}

func TestTemplatePreviewUnknownTemplateShowsError(t *testing.T) {
	s := templateTestServer(t, ukCfg("gdpr"))

	req := httptest.NewRequest(http.MethodGet, "/api/template/preview?template=nope", nil)
	rec := httptest.NewRecorder()
	s.handleAPITemplatePreview(rec, req)

	if !strings.Contains(rec.Body.String(), "Unknown template") {
		t.Errorf("expected an error message, got: %s", rec.Body.String())
	}
}

// Every template offered in the dropdown must be renderable and carry a
// description, or the user is choosing between opaque names - or picking one
// that errors the moment it's previewed.
func TestAllOfferedTemplatesAreUsable(t *testing.T) {
	s := templateTestServer(t, ukCfg("gdpr"))

	for _, name := range emaTemplate.TemplateNames() {
		if templateDescriptions[name] == "" {
			t.Errorf("template %q has no description for the dropdown", name)
		}
		if p := s.previewFor(httptest.NewRequest(http.MethodGet, "/", nil), name); p.Error != "" {
			t.Errorf("template %q failed to preview: %s", name, p.Error)
		}
	}
}
