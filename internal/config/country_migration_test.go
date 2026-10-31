package config

import (
	"os"
	"path/filepath"
	"runtime"
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

// A failed backup or rewrite must not lose the in-memory migration or block loading.
func TestLoadCountryMigrationSurvivesWriteFailures(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs POSIX permissions and a non-root user")
	}
	body := "profiles:\n  - id: default\n    first_name: A\n    last_name: B\n    email: a@b.co\n    country: Germany\n"

	t.Run("backup fails", func(t *testing.T) {
		p := writeCfg(t, body)
		dir := filepath.Dir(p)
		if err := os.Chmod(dir, 0o500); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
		cfg, err := Load(p)
		if err != nil || cfg.Profiles[0].Country != "DE" {
			t.Fatalf("load = %v, %v", cfg, err)
		}
		if got, _ := os.ReadFile(p); string(got) != body {
			t.Error("config must be left untouched when the backup can't be written")
		}
	})

	t.Run("rewrite fails", func(t *testing.T) {
		p := writeCfg(t, body)
		if err := os.WriteFile(p+".1x.bak", []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(p, 0o400); err != nil {
			t.Fatal(err)
		}
		cfg, err := Load(p)
		if err != nil || cfg.Profiles[0].Country != "DE" {
			t.Fatalf("load = %v, %v", cfg, err)
		}
	})
}

func TestLoadMigratesLegacyProfileBlockAndDropsProvider(t *testing.T) {
	orig := "profile:\n  first_name: Old\n  last_name: Style\n  email: o@s.co\n  country: UK\nemail:\n  provider: smtp\n  from: o@s.co\n  smtp:\n    host: smtp.example.com\n    port: 587\n"
	p := writeCfg(t, orig)

	cfg, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Profiles) != 1 || cfg.Profiles[0].ID != DefaultProfileID || cfg.Profiles[0].FirstName != "Old" || cfg.Profiles[0].Country != "GB" {
		t.Fatalf("profiles = %+v", cfg.Profiles)
	}
	if bak, _ := os.ReadFile(p + ".1x.bak"); string(bak) != orig {
		t.Error("original config not backed up")
	}
	saved, _ := os.ReadFile(p)
	if strings.Contains(string(saved), "provider:") || strings.Contains(string(saved), "\nprofile:") {
		t.Errorf("legacy keys survived the rewrite:\n%s", saved)
	}
	if !strings.Contains(string(saved), "profiles:") {
		t.Errorf("profiles: missing after rewrite:\n%s", saved)
	}
}

func TestLoadIgnoresLegacyProfileWhenProfilesExist(t *testing.T) {
	p := writeCfg(t, "profile:\n  first_name: Old\nprofiles:\n  - id: me\n    first_name: New\n    last_name: X\n    email: n@x.co\n")
	cfg, err := Load(p)
	if err != nil || len(cfg.Profiles) != 1 || cfg.Profiles[0].FirstName != "New" {
		t.Fatalf("load = %+v, %v", cfg, err)
	}
}
