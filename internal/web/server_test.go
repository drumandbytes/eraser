package web

import (
	"fmt"
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

	got := s.getBrokersWithStatus("default", "", "", "", "", false)

	if len(got) != 1 || got[0].ID != "beenverified" {
		t.Errorf("expected only beenverified to survive exclusion, got %+v", got)
	}
}
