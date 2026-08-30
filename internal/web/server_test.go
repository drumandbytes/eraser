package web

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/eraser-privacy/eraser/internal/broker"
	"github.com/eraser-privacy/eraser/internal/config"
	emaTemplate "github.com/eraser-privacy/eraser/internal/template"
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

// TestServerConfigConcurrentAccess exercises Server.config (an
// atomic.Pointer[config.Config]) with concurrent readers (getConfig, as
// every handler does) and writers (s.config.Store, as handleSettingsInbox
// and handleSetupComplete do via load-copy-mutate-store). Run with
// `go test -race`: before the atomic.Pointer fix, Server.config was a plain
// *config.Config mutated in place, which the race detector flags as soon as
// a read and a write actually overlap - which is exactly what this test
// forces by running many of each concurrently and without any external
// synchronization between them.
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

// TestGetBrokersWithStatusRespectsExclusions is a regression test:
// excluded_brokers/excluded_categories used to only be enforced by the CLI's
// `send` command (via broker.Filter) - the web UI's brokers list and bulk
// "Send to All" both go through getBrokersWithStatus instead, which never
// looked at either option, so a configured exclusion silently had no effect
// there.
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

	got := s.getBrokersWithStatus("default", brokerQuery{})

	if len(got) != 1 || got[0].ID != "beenverified" {
		t.Errorf("expected only beenverified to survive exclusion, got %+v", got)
	}
}

func TestGetBrokersWithStatusPriorityFilter(t *testing.T) {
	s := newTestServer(t, testConfig())
	s.brokerDB.Brokers = []broker.Broker{
		{ID: "spokeo", Name: "Spokeo", Region: "us", Category: "people-search", Priority: "high"},
		{ID: "acxiom", Name: "Acxiom", Region: "us", Category: "marketing", Priority: "high"},
		{ID: "smalltown", Name: "SmallTown Data", Region: "us", Category: "marketing", Priority: "low"},
		{ID: "untagged", Name: "Untagged Broker", Region: "us", Category: "marketing"},
	}

	tests := []struct {
		name  string
		query brokerQuery
		want  []string
	}{
		{"blank priority returns everything", brokerQuery{}, []string{"spokeo", "acxiom", "smalltown", "untagged"}},
		{"high only", brokerQuery{Priority: "high"}, []string{"spokeo", "acxiom"}},
		{"low only", brokerQuery{Priority: "low"}, []string{"smalltown"}},
		{"case-insensitive", brokerQuery{Priority: "HIGH"}, []string{"spokeo", "acxiom"}},
		{"unrecognized priority is treated as no filter, like a bogus category", brokerQuery{Priority: "urgent"}, []string{"spokeo", "acxiom", "smalltown", "untagged"}},
		{"medium matches nothing here", brokerQuery{Priority: "medium"}, nil},
		{"combines with category", brokerQuery{Priority: "high", Category: "marketing"}, []string{"acxiom"}},
		{"combines with a category that has no high brokers", brokerQuery{Priority: "low", Category: "people-search"}, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := s.getBrokersWithStatus("default", tt.query)

			var ids []string
			for _, b := range got {
				ids = append(ids, b.ID)
			}
			if !reflect.DeepEqual(ids, tt.want) {
				t.Errorf("getBrokersWithStatus(%+v) = %v, want %v", tt.query, ids, tt.want)
			}
		})
	}
}

// TestBrokersPageRendersPriorityFilter exercises the real templates end to
// end. The brokers page's table and partials/broker-list.html are
// hand-duplicated markup (see docs/code-patterns.md), so this is the guard
// that both actually parse and that the priority filter survives the trip
// through the handler - a template typo in either one is otherwise only
// visible by loading the page by hand.
func TestBrokersPageRendersPriorityFilter(t *testing.T) {
	s := newTestServer(t, testConfig())
	s.brokerDB.Brokers = []broker.Broker{
		{ID: "spokeo", Name: "Spokeo", Email: "privacy@spokeo.com", Region: "us", Category: "people-search", Priority: "high"},
		{ID: "smalltown", Name: "SmallTown Data", Email: "p@smalltown.example", Region: "us", Category: "marketing", Priority: "low"},
	}
	router := s.setupRouter()

	tests := []struct {
		name         string
		url          string
		wantContains []string
		wantMissing  string
	}{
		{"page, unfiltered", "/brokers", []string{"All Priorities", "Spokeo", "SmallTown Data"}, ""},
		{"page, high only", "/brokers?priority=high", []string{"Spokeo"}, "SmallTown Data"},
		{"htmx fragment, high only", "/api/brokers?priority=high", []string{"Spokeo"}, "SmallTown Data"},
		{"htmx fragment, low only", "/api/brokers?priority=low", []string{"SmallTown Data"}, "Spokeo"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.url, nil)
			req.Host = "127.0.0.1"
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("GET %s = %d, want 200", tt.url, rec.Code)
			}
			body := rec.Body.String()
			for _, want := range tt.wantContains {
				if !strings.Contains(body, want) {
					t.Errorf("GET %s: response is missing %q", tt.url, want)
				}
			}
			if tt.wantMissing != "" && strings.Contains(body, tt.wantMissing) {
				t.Errorf("GET %s: priority filter let %q through", tt.url, tt.wantMissing)
			}
		})
	}
}
