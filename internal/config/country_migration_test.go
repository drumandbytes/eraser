package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeCfg(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadMigratesCountryToISOWithBackup(t *testing.T) {
	orig := "profiles:\n  - id: default\n    first_name: A\n    last_name: B\n    email: a@b.co\n    country: Germany\n  - id: two\n    first_name: C\n    last_name: D\n    email: c@d.co\n    country: DE\n"
	p := writeCfg(t, orig)

	cfg, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Profiles[0].Country != "DE" || cfg.Profiles[1].Country != "DE" {
		t.Errorf("countries = %q, %q", cfg.Profiles[0].Country, cfg.Profiles[1].Country)
	}
	bak, err := os.ReadFile(p + ".1x.bak")
	if err != nil || string(bak) != orig {
		t.Fatalf("backup missing or altered: %v", err)
	}
	saved, _ := os.ReadFile(p)
	if !strings.Contains(string(saved), "country: DE") || strings.Contains(string(saved), "Germany") {
		t.Errorf("config not rewritten:\n%s", saved)
	}

	// Second load: nothing to migrate, backup untouched.
	if _, err := Load(p); err != nil {
		t.Fatal(err)
	}
	if again, _ := os.ReadFile(p + ".1x.bak"); string(again) != orig {
		t.Error("backup was overwritten")
	}
}

func TestLoadKeepsUnresolvableCountry(t *testing.T) {
	p := writeCfg(t, "profiles:\n  - id: default\n    first_name: A\n    last_name: B\n    email: a@b.co\n    country: Atlantis\n")
	cfg, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Profiles[0].Country != "Atlantis" {
		t.Errorf("country = %q", cfg.Profiles[0].Country)
	}
	if _, err := os.Stat(p + ".1x.bak"); err == nil {
		t.Error("no migration happened, so no backup expected")
	}
}
