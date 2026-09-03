package web

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/drumandbytes/eraser/internal/broker"
	"github.com/drumandbytes/eraser/internal/config"
	"github.com/drumandbytes/eraser/internal/history"
)

// smokeServer is a Server wired up the way a running instance is: a real
// (temp) history store, a small broker DB, a saved config, and a couple of
// profiles. Enough for the page handlers to run their real code paths.
func smokeServer(t *testing.T) *Server {
	t.Helper()

	cfg := testConfig("default", "spouse")
	cfg.Profile = config.Profile{FirstName: "Test", LastName: "User", Email: "test@example.com"}
	s := newTestServer(t, cfg)
	s.configPath = filepath.Join(t.TempDir(), "config.yaml")
	if err := config.Save(s.configPath, cfg); err != nil {
		t.Fatalf("config.Save: %v", err)
	}

	store, err := history.NewStore(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatalf("history.NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	s.historyStore = store

	s.brokerDB.Brokers = []broker.Broker{
		{ID: "spokeo", Name: "Spokeo", Email: "privacy@spokeo.com", Region: "us", Category: "people-search"},
		{ID: "acme-eu", Name: "Acme EU", Email: "dpo@acme.example", Region: "eu", Category: "marketing"},
		{ID: "noemail", Name: "No Email Broker", Region: "us", Category: "marketing", OptOutURL: "https://example.com/opt-out"},
	}
	return s
}

// bodyLooksLikeTemplateError catches a broken html/template render leaking into
// the response instead of a real page.
func bodyLooksLikeTemplateError(body string) (string, bool) {
	for _, sig := range []string{
		"Template error",
		"executing \"",
		"can't evaluate field",
		"error calling",
		"<no value>",
		"incomplete or empty template",
	} {
		if strings.Contains(body, sig) {
			return sig, true
		}
	}
	return "", false
}

func TestGETRoutesRenderWithoutError(t *testing.T) {
	s := smokeServer(t)
	router := s.setupRouter()

	// Fixed-path GET routes that should render a full page (200) for a
	// configured install.
	routes := []string{
		"/",
		"/brokers",
		"/brokers?search=spokeo&region=us&category=people-search&status=pending",
		"/brokers?show_excluded=true&missing_email=true",
		"/history",
		"/history?status=failed",
		"/settings",
		"/settings/profiles/new",
		"/pipeline",
		"/tasks",
		"/brokers/spokeo/email",
		"/settings/profiles/default/edit",
		"/api/brokers",
		"/api/brokers/spokeo/status",
		"/api/job/active",
		"/api/pipeline/responses",
		"/setup",
		"/setup/profile",
		"/setup/email",
	}

	for _, route := range routes {
		t.Run(route, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, route, nil)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if rec.Code >= 500 {
				t.Fatalf("%s: %d\n%s", route, rec.Code, rec.Body.String())
			}
			// Setup guards legitimately redirect (302) between wizard steps;
			// everything else should be a 200.
			setupRedirect := strings.HasPrefix(route, "/setup") && rec.Code == http.StatusFound
			if rec.Code != http.StatusOK && !setupRedirect {
				t.Fatalf("%s: unexpected status %d", route, rec.Code)
			}
			if sig, bad := bodyLooksLikeTemplateError(rec.Body.String()); bad {
				t.Fatalf("%s: response contains template-error signature %q\n%s", route, sig, rec.Body.String())
			}
		})
	}

	// Routes that are intentional redirects.
	for _, route := range []string{"/forms"} {
		req := httptest.NewRequest(http.MethodGet, route, nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusFound {
			t.Errorf("%s: got %d, want 302", route, rec.Code)
		}
	}
}

// A brand-new install (no config yet) must not 500 anywhere in the wizard, and
// the dashboard must bounce to /setup.
func TestUnconfiguredInstallRoutes(t *testing.T) {
	s := newTestServer(t, nil)
	s.configPath = filepath.Join(t.TempDir(), "config.yaml")
	router := s.setupRouter()

	for _, route := range []string{"/", "/setup", "/setup/profile", "/setup/email", "/setup/test", "/setup/complete"} {
		req := httptest.NewRequest(http.MethodGet, route, nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code >= 500 {
			t.Fatalf("%s on unconfigured install: %d\n%s", route, rec.Code, rec.Body.String())
		}
		if sig, bad := bodyLooksLikeTemplateError(rec.Body.String()); bad {
			t.Fatalf("%s: template-error signature %q\n%s", route, sig, rec.Body.String())
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/setup" {
		t.Fatalf("dashboard on unconfigured install: got %d -> %q, want 302 -> /setup", rec.Code, rec.Header().Get("Location"))
	}
}

// The 404 path also renders through the router without a 500.
func TestUnknownRouteIs404(t *testing.T) {
	s := smokeServer(t)
	router := s.setupRouter()

	req := httptest.NewRequest(http.MethodGet, "/does-not-exist", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("got %d, want 404", rec.Code)
	}
}
