package web

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/drumandbytes/eraser/internal/broker"
	"github.com/drumandbytes/eraser/internal/config"
	emaTemplate "github.com/drumandbytes/eraser/internal/template"
)

// newTestServer builds a minimal *Server suitable for unit tests that don't
// need a real broker file, history database, or HTTP listener. It exercises
// the real NewServer constructor (including template parsing) so tests stay
// honest about what construction actually requires.
func newTestServer(t *testing.T, cfg *config.Config) *Server {
	t.Helper()

	tmplEngine, err := emaTemplate.NewEngine()
	if err != nil {
		t.Fatalf("template.NewEngine: %v", err)
	}

	s, err := NewServer(0, cfg, "", &broker.BrokerDatabase{}, nil, tmplEngine)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	return s
}

func testConfig(profileIDs ...string) *config.Config {
	if len(profileIDs) == 0 {
		return &config.Config{
			Profile: config.Profile{FirstName: "Test", LastName: "User", Email: "test@example.com"},
		}
	}
	cfg := &config.Config{}
	for _, id := range profileIDs {
		cfg.Profiles = append(cfg.Profiles, config.NamedProfile{
			ID: id,
			Profile: config.Profile{
				FirstName: "Test",
				LastName:  id,
				Email:     id + "@example.com",
			},
		})
	}
	return cfg
}

// Concurrent getConfig reads and Store writes; run with -race (config used to
// be a plain pointer mutated in place).
func TestServerConfigConcurrentAccess(t *testing.T) {
	s := newTestServer(t, testConfig())

	const readers = 8
	const writers = 4
	const iterations = 500

	var wg sync.WaitGroup
	var reads int64

	// Readers: exactly what handlers do via s.getConfig().
	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				cfg := s.getConfig()
				if cfg == nil {
					t.Errorf("getConfig returned nil")
					return
				}
				// Touch a field to make sure we actually dereference the
				// pointer (and would race on a plain *config.Config if a
				// writer is mutating the same struct in place).
				_ = cfg.Options.Template
				_ = cfg.GetProfiles()
				atomic.AddInt64(&reads, 1)
			}
		}()
	}

	// Writers: load -> copy -> mutate -> store, same pattern as
	// handleSettingsInbox/handleSetupComplete.
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				old := s.getConfig()
				updated := *old // copy
				updated.Options.Template = fmt.Sprintf("writer-%d-iter-%d", w, j)
				s.config.Store(&updated)
			}
		}(i)
	}

	wg.Wait()

	if got := atomic.LoadInt64(&reads); got != readers*iterations {
		t.Fatalf("expected %d successful reads, got %d", readers*iterations, got)
	}

	// Sanity: config is still readable and non-nil after all the churn.
	if cfg := s.getConfig(); cfg == nil {
		t.Fatal("getConfig returned nil after concurrent writes")
	}
}

// TestServerConfigLoadCopyMutateStoreIsolation confirms that storing a
// mutated copy never affects a config pointer obtained by an earlier
// getConfig() call - i.e. writers really do produce a new *config.Config
// rather than mutating the one readers may still be holding.
func TestServerConfigLoadCopyMutateStoreIsolation(t *testing.T) {
	s := newTestServer(t, testConfig())

	before := s.getConfig()
	beforeTemplate := before.Options.Template

	updated := *before
	updated.Options.Template = "changed"
	s.config.Store(&updated)

	if before.Options.Template != beforeTemplate {
		t.Fatalf("earlier config snapshot was mutated in place: got %q, want %q", before.Options.Template, beforeTemplate)
	}

	after := s.getConfig()
	if after.Options.Template != "changed" {
		t.Fatalf("getConfig after Store: got %q, want %q", after.Options.Template, "changed")
	}
}

// The web brokers list and "Send to All" must honor excluded_brokers/categories.
func TestGetBrokersWithStatusRespectsExclusions(t *testing.T) {
	cfg := testConfig()
	cfg.Options.ExcludedBrokers = []string{"spokeo"}
	cfg.Options.ExcludedCategories = []string{"requires-id"}
	s := newTestServer(t, cfg)
	s.brokerDB.Brokers = []broker.Broker{
		{ID: "spokeo", Name: "Spokeo", Region: "us", Category: "people-search"},
		{ID: "altisource-holdings", Name: "Altisource Holdings, LLC", Region: "us", Category: "requires-id"},
		{ID: "beenverified", Name: "BeenVerified", Region: "us", Category: "people-search"},
	}

	got := s.getBrokersWithStatus("default", "", "", "", "", nil, nil, false, false)

	if len(got) != 1 || got[0].ID != "beenverified" {
		t.Errorf("expected only beenverified to survive exclusion, got %+v", got)
	}
}

// "Show excluded" returns excluded brokers flagged Excluded.
func TestGetBrokersWithStatusShowExcludedIncludesAndMarksThem(t *testing.T) {
	cfg := testConfig()
	cfg.Options.ExcludedBrokers = []string{"spokeo"}
	cfg.Options.ExcludedCategories = []string{"requires-id"}
	s := newTestServer(t, cfg)
	s.brokerDB.Brokers = []broker.Broker{
		{ID: "spokeo", Name: "Spokeo", Region: "us", Category: "people-search"},
		{ID: "altisource-holdings", Name: "Altisource Holdings, LLC", Region: "us", Category: "requires-id"},
		{ID: "beenverified", Name: "BeenVerified", Region: "us", Category: "people-search"},
	}

	got := s.getBrokersWithStatus("default", "", "", "", "", nil, nil, false, true)

	if len(got) != 3 {
		t.Fatalf("expected all 3 brokers with showExcluded=true, got %d: %+v", len(got), got)
	}
	excluded := map[string]bool{}
	for _, b := range got {
		excluded[b.ID] = b.Excluded
	}
	if !excluded["spokeo"] || !excluded["altisource-holdings"] {
		t.Errorf("expected spokeo and altisource-holdings marked Excluded, got %+v", excluded)
	}
	if excluded["beenverified"] {
		t.Errorf("beenverified should not be marked Excluded, got %+v", excluded)
	}
}

// drumandbytes/eraser#1: on first /setup there's no config, and a missing
// Profiles key made layout.html's len fail.
func TestRenderWithCSRFHandlesNilConfig(t *testing.T) {
	s := newTestServer(t, nil)

	req := httptest.NewRequest(http.MethodGet, "/setup", nil)
	rec := httptest.NewRecorder()
	s.handleSetupWelcome(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "Template error") {
		t.Errorf("response contains a template execution error: %s", rec.Body.String())
	}
}

func TestHostAllowed(t *testing.T) {
	cases := []struct {
		host string
		want bool
	}{
		{"127.0.0.1:8080", true},
		{"127.0.0.1", true},
		{"localhost:8080", true},
		{"LOCALHOST", true},
		{"[::1]:8080", true},
		{"::1", true},
		// The DNS-rebinding case this exists to catch: a hostname that
		// resolves to 127.0.0.1 but isn't one of the literal loopback names.
		{"attacker.example:8080", false},
		{"evil.localhost.attacker.com", false},
		{"0.0.0.0:8080", false},
	}
	for _, tc := range cases {
		if got := hostAllowed(tc.host); got != tc.want {
			t.Errorf("hostAllowed(%q) = %v, want %v", tc.host, got, tc.want)
		}
	}
}

func TestRequireLoopbackHostBlocksNonLoopback(t *testing.T) {
	handler := requireLoopbackHost(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "attacker.example"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("non-loopback Host: got status %d, want 403", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "127.0.0.1:8080"
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("loopback Host: got status %d, want 200", rec.Code)
	}
}
