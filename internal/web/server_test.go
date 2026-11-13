package web

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/drumandbytes/delistd/internal/broker"
	"github.com/drumandbytes/delistd/internal/config"
	"github.com/drumandbytes/delistd/internal/schedule"
	emaTemplate "github.com/drumandbytes/delistd/internal/template"
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
	// Keep the schedule lock/state and the OS scheduler off the real machine.
	s.dataDir = t.TempDir()
	s.osInstalled = func() bool { return false }
	s.installOS = func(schedule.Job) error { t.Error("unexpected OS install"); return nil }
	s.removeOS = func() error { t.Error("unexpected OS remove"); return nil }
	return s
}

func testConfig(profileIDs ...string) *config.Config {
	if len(profileIDs) == 0 {
		return &config.Config{
			Profiles: []config.NamedProfile{{ID: config.DefaultProfileID, Profile: config.Profile{FirstName: "Test", LastName: "User", Email: "test@example.com"}}},
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
	s.brokers().Brokers = []broker.Broker{
		{ID: "spokeo", Name: "Spokeo", Region: "us", Category: "people-search"},
		{ID: "altisource-holdings", Name: "Altisource Holdings, LLC", Region: "us", Category: "requires-id"},
		{ID: "beenverified", Name: "BeenVerified", Region: "us", Category: "people-search"},
	}

	got := s.getBrokersWithStatus("default", "", "", "", "", nil, nil, false, false)

	if len(got) != 1 || got[0].ID != "beenverified" {
		t.Errorf("expected only beenverified to survive exclusion, got %+v", got)
	}
}

// A broker scoped to other countries is hidden for a profile outside them.
func TestGetBrokersWithStatusRespectsCountries(t *testing.T) {
	cfg := testConfig("default")
	cfg.Profiles[0].Country = "France"
	s := newTestServer(t, cfg)
	s.brokers().Brokers = []broker.Broker{
		{ID: "de-only", Name: "DE Only", Region: "eu", Countries: []string{"DE"}},
		{ID: "anywhere", Name: "Anywhere", Region: "eu"},
	}

	got := s.getBrokersWithStatus("default", "", "", "", "", nil, nil, false, false)

	if len(got) != 1 || got[0].ID != "anywhere" {
		t.Errorf("expected only anywhere for a French profile, got %+v", got)
	}
}

// "Show excluded" returns excluded brokers flagged Excluded.
func TestGetBrokersWithStatusShowExcludedIncludesAndMarksThem(t *testing.T) {
	cfg := testConfig()
	cfg.Options.ExcludedBrokers = []string{"spokeo"}
	cfg.Options.ExcludedCategories = []string{"requires-id"}
	s := newTestServer(t, cfg)
	s.brokers().Brokers = []broker.Broker{
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

// drumandbytes/delistd#1: on first /setup there's no config, and a missing
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

func TestServerHostAllowed(t *testing.T) {
	s := &Server{ListenHost: "127.0.0.1"}

	// Default loopback listener: loopback allowed, private IPs and others rejected
	if !s.hostAllowed("localhost:8080") || !s.hostAllowed("127.0.0.1:8080") {
		t.Errorf("expected loopback to be allowed on loopback listener")
	}
	if s.hostAllowed("192.168.178.220:9890") {
		t.Errorf("expected private IP to be rejected on loopback listener without AllowedHosts")
	}

	// AllowedHosts with 'lan' or 'private': private/local IPs allowed, external rejected
	sLAN := &Server{ListenHost: "0.0.0.0", AllowedHosts: []string{"lan"}}
	casesLAN := []struct {
		host string
		want bool
	}{
		{"localhost:8080", true},
		{"127.0.0.1:8080", true},
		{"192.168.178.220:9890", true},
		{"10.0.1.5:8080", true},
		{"172.16.2.3:8080", true},
		{"100.80.1.2:8080", true}, // Tailscale / CGNAT
		{"[fe80::1]:8080", true},  // IPv6 link-local
		{"[fc00::1]:8080", true},  // IPv6 ULA
		{"attacker.example:8080", false},
		{"8.8.8.8:8080", false},
	}
	for _, tc := range casesLAN {
		if got := sLAN.hostAllowed(tc.host); got != tc.want {
			t.Errorf("sLAN.hostAllowed(%q) = %v, want %v", tc.host, got, tc.want)
		}
	}

	// 'local': single-label and local-only names, never public hostnames or IPs
	sLocal := &Server{ListenHost: "0.0.0.0", AllowedHosts: []string{"local"}}
	for host, want := range map[string]bool{
		"tower:9890":            true,
		"unraid.local:9890":     true,
		"nas.lan":               true,
		"box.home.arpa:8080":    true,
		"svc.internal:8080":     true,
		"localhost:8080":        true, // loopback is always allowed
		"attacker.example:8080": false,
		"evil.example.com.":     false, // a trailing dot doesn't make it local
		"local.attacker.com":    false, // only a suffix counts
		"8.8.8.8:8080":          false, // no IP matches 'local' (that's 'lan')
		"192.168.1.5:8080":      false,
		"":                      false,
	} {
		if got := sLocal.hostAllowed(host); got != want {
			t.Errorf("local hostAllowed(%q) = %v, want %v", host, got, want)
		}
	}
	// the container default: private IPs plus local names, nothing public
	sDefault := &Server{ListenHost: "0.0.0.0", AllowedHosts: ParseAllowedHosts("lan,local")}
	for host, want := range map[string]bool{"192.168.178.220:9890": true, "tower:9890": true, "tower.local": true, "evil.example.com": false, "8.8.8.8": false} {
		if got := sDefault.hostAllowed(host); got != want {
			t.Errorf("lan,local hostAllowed(%q) = %v, want %v", host, got, want)
		}
	}

	// AllowedHosts with wildcard '*' allows any host
	sWildcard := &Server{ListenHost: "127.0.0.1", AllowedHosts: []string{"*"}}
	if !sWildcard.hostAllowed("attacker.example:8080") || !sWildcard.hostAllowed("unraid.local:9890") {
		t.Errorf("expected wildcard AllowedHosts to permit all hosts")
	}

	// AllowedHosts with specific names and patterns
	sConfigured := &Server{
		ListenHost:   "127.0.0.1",
		AllowedHosts: []string{"unraid.local", "*.lan", "10.0.0.0/8"},
	}
	if !sConfigured.hostAllowed("unraid.local:9890") {
		t.Errorf("expected unraid.local to be allowed")
	}
	if !sConfigured.hostAllowed("mybox.lan:8080") {
		t.Errorf("expected *.lan wildcard domain to be allowed")
	}
	if !sConfigured.hostAllowed("10.5.6.7:8080") {
		t.Errorf("expected 10.0.0.0/8 CIDR to be allowed")
	}
	if sConfigured.hostAllowed("other.local:8080") {
		t.Errorf("expected other.local to be rejected")
	}
	if sConfigured.hostAllowed("attacker.example:8080") {
		t.Errorf("expected attacker.example to be rejected")
	}
}

func TestRequireAllowedHostMiddleware(t *testing.T) {
	s := &Server{ListenHost: "0.0.0.0", AllowedHosts: []string{"unraid.local", "192.168.178.220"}}
	handler := s.requireAllowedHost(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Allowed explicit IP
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "192.168.178.220:9890"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("allowed IP Host: got %d, want 200", rec.Code)
	}

	// Allowed explicit hostname
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "unraid.local:9890"
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("allowed hostname Host: got %d, want 200", rec.Code)
	}

	// Rejected external host
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "attacker.example"
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("unauthorized Host: got %d, want 403", rec.Code)
	}
}

func TestParseAllowedHosts(t *testing.T) {
	if got := ParseAllowedHosts(""); got != nil {
		t.Errorf("expected nil for empty string, got %v", got)
	}
	got := ParseAllowedHosts(" 192.168.1.1, unraid.local , *.lan ")
	want := []string{"192.168.1.1", "unraid.local", "*.lan"}
	if len(got) != len(want) {
		t.Fatalf("ParseAllowedHosts: got %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("ParseAllowedHosts[%d]: got %q, want %q", i, got[i], want[i])
		}
	}
}

func TestCountryHiddenCountsOutOfScopeBrokers(t *testing.T) {
	cfg := testConfig("default")
	cfg.Profiles[0].Country = "France"
	s := newTestServer(t, cfg)
	s.brokers().Brokers = []broker.Broker{
		{ID: "de-only", Name: "DE Only", Countries: []string{"DE"}},
		{ID: "anywhere", Name: "Anywhere"},
	}
	if n, c := s.countryHidden("default"); n != 1 || c != "France" {
		t.Errorf("countryHidden = %d, %q; want 1, France", n, c)
	}
	cfg.Profiles[0].Country = ""
	if n, _ := s.countryHidden("default"); n != 0 {
		t.Errorf("unset country should hide nothing, got %d", n)
	}
	if n, _ := s.countryHidden("nobody"); n != 0 {
		t.Errorf("unknown profile should hide nothing, got %d", n)
	}
	s.config.Store(nil)
	if n, _ := s.countryHidden("default"); n != 0 {
		t.Errorf("no config should hide nothing, got %d", n)
	}
}
