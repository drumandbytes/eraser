package web

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/drumandbytes/eraser/internal/config"
)

func reloadServer(t *testing.T) *Server {
	t.Helper()
	cfg := testConfig("default")
	cfg.Profiles[0].Country = "FR"
	s := newTestServer(t, cfg)
	s.configPath = filepath.Join(t.TempDir(), "config.yaml")
	if err := s.saveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	return s
}

// writeOutside edits the file the way the CLI would, then bumps the mtime so
// coarse-granularity filesystems still see a change.
func writeOutside(t *testing.T, s *Server, mutate func(*config.Config)) {
	t.Helper()
	cfg, err := config.Load(s.configPath)
	if err != nil {
		t.Fatal(err)
	}
	mutate(cfg)
	if err := config.Save(s.configPath, cfg); err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(s.configPath, later, later); err != nil {
		t.Fatal(err)
	}
}

func TestRefreshConfigPicksUpCLIEdits(t *testing.T) {
	s := reloadServer(t)
	s.refreshConfig()
	if got := s.getConfig().Profiles[0].Country; got != "FR" {
		t.Fatalf("unchanged file should keep config, got %q", got)
	}

	writeOutside(t, s, func(c *config.Config) { c.Profiles[0].Country = "LV" })
	s.refreshConfig()
	if got := s.getConfig().Profiles[0].Country; got != "LV" {
		t.Errorf("country after outside edit = %q, want LV", got)
	}
}

func TestRefreshConfigIgnoresOwnSavesAndBadFiles(t *testing.T) {
	s := reloadServer(t)
	cur := s.getConfig()

	own := *cur
	own.Options.Template = "ccpa"
	if err := s.saveConfig(&own); err != nil {
		t.Fatal(err)
	}
	s.config.Store(&own)
	s.refreshConfig()
	if s.getConfig() != &own {
		t.Error("the web UI's own save must not trigger a reload")
	}

	if err := os.WriteFile(s.configPath, []byte("profiles: [unclosed"), 0o600); err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(2 * time.Second)
	_ = os.Chtimes(s.configPath, later, later)
	s.refreshConfig()
	if s.getConfig() != &own {
		t.Error("an unparseable file must keep the current config")
	}

	if err := os.Remove(s.configPath); err != nil {
		t.Fatal(err)
	}
	s.refreshConfig()
	if s.getConfig() != &own {
		t.Error("a missing file must keep the current config")
	}
}

func TestReloadMiddlewareRefreshesBeforeHandler(t *testing.T) {
	s := reloadServer(t)
	writeOutside(t, s, func(c *config.Config) { c.Profiles[0].Country = "DE" })

	var seen string
	h := s.reloadConfigOnChange(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = s.getConfig().Profiles[0].Country
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	if seen != "DE" {
		t.Errorf("handler saw %q, want DE", seen)
	}

	noPath := newTestServer(t, testConfig("default"))
	noPath.refreshConfig() // no configPath: must be a no-op, not a panic
}

// First run: no config.yaml when serve starts, then `eraser init` creates one.
func TestRefreshConfigPicksUpFileCreatedAfterStart(t *testing.T) {
	s := newTestServer(t, &config.Config{})
	s.configPath = filepath.Join(t.TempDir(), "config.yaml")
	s.cfgStamp = "none"
	s.refreshConfig()
	if s.getConfig().HasProfile() {
		t.Fatal("no file yet: nothing to load")
	}

	cfg := testConfig("default")
	if err := config.Save(s.configPath, cfg); err != nil {
		t.Fatal(err)
	}
	s.refreshConfig()
	if !s.getConfig().HasProfile() {
		t.Error("config created by the CLI after start was not loaded")
	}
}
