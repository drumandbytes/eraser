package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTestConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}
	return path
}

const minimalConfig = `
profile:
  first_name: Test
  last_name: User
  email: test@example.com
email:
  provider: smtp
  from: test@example.com
  smtp:
    host: smtp.example.com
    port: 465
`

func TestLoadPreservesExplicitBrowserHeadlessFalse(t *testing.T) {
	path := writeTestConfig(t, minimalConfig+"pipeline:\n  browser_headless: false\n")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Pipeline.Headless() != false {
		t.Errorf("expected Headless() to stay false when explicitly set, got true")
	}

	// Round-trip through Save to make sure re-saving doesn't lose it either -
	// this is what `eraser init` now does on every update-mode run.
	savedPath := filepath.Join(t.TempDir(), "resaved.yaml")
	if err := Save(savedPath, cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}
	reloaded, err := Load(savedPath)
	if err != nil {
		t.Fatalf("reload after save: %v", err)
	}
	if reloaded.Pipeline.Headless() != false {
		t.Errorf("expected Headless() to survive a save/reload round-trip, got true")
	}
}

func TestLoadDefaultsBrowserHeadlessTrueWhenUnset(t *testing.T) {
	path := writeTestConfig(t, minimalConfig)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Pipeline.Headless() != true {
		t.Errorf("expected Headless() to default to true when unset, got false")
	}
	if cfg.Pipeline.BrowserHeadless != nil {
		t.Errorf("expected BrowserHeadless to stay nil when unset, got %v", *cfg.Pipeline.BrowserHeadless)
	}
}

func TestLoadPreservesExplicitBrowserHeadlessTrue(t *testing.T) {
	path := writeTestConfig(t, minimalConfig+"pipeline:\n  browser_headless: true\n")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Pipeline.Headless() != true {
		t.Errorf("expected Headless() to stay true when explicitly set, got false")
	}
	if cfg.Pipeline.BrowserHeadless == nil || !*cfg.Pipeline.BrowserHeadless {
		t.Errorf("expected BrowserHeadless to be a non-nil true, got %v", cfg.Pipeline.BrowserHeadless)
	}
}

func TestSlugifyProfileID(t *testing.T) {
	tests := []struct {
		name     string
		first    string
		last     string
		existing []NamedProfile
		want     string
	}{
		{"basic", "Jane", "Doe", nil, "jane-doe"},
		{"diacritics and case", "Māris", "Popēns", nil, "m-ris-pop-ns"},
		{"collision appends -2", "Jane", "Doe", []NamedProfile{{ID: "jane-doe"}}, "jane-doe-2"},
		{"collision is case-insensitive", "Jane", "Doe", []NamedProfile{{ID: "JANE-DOE"}}, "jane-doe-2"},
		{"multiple collisions increment", "Jane", "Doe", []NamedProfile{{ID: "jane-doe"}, {ID: "jane-doe-2"}}, "jane-doe-3"},
		{"empty name falls back", "", "", nil, "profile"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SlugifyProfileID(tt.first, tt.last, tt.existing)
			if got != tt.want {
				t.Errorf("SlugifyProfileID(%q, %q, %v) = %q, want %q", tt.first, tt.last, tt.existing, got, tt.want)
			}
		})
	}
}

func TestSlugifyID(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"plain", "spouse", "spouse"},
		{"spaces and punctuation", "María López!", "mar-a-l-pez"},
		{"already valid", "kid1", "kid1"},
		{"empty falls back", "", "profile"},
		{"only symbols falls back", "!!!", "profile"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SlugifyID(tt.in)
			if got != tt.want {
				t.Errorf("SlugifyID(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

const multiProfileMailConfig = `
profiles:
  - id: default
    first_name: Test
    last_name: User
    email: test@example.com
  - id: spouse
    first_name: Spouse
    last_name: User
    email: spouse@example.com
    mail:
      email:
        provider: smtp
        from: spouse@gmail.com
        smtp:
          host: smtp.gmail.com
          port: 465
          username: spouse@gmail.com
          password: app-password
      inbox:
        enabled: true
        provider: gmail
        email: spouse@gmail.com
        password: app-password
email:
  provider: smtp
  from: test@example.com
  smtp:
    host: smtp.example.com
    port: 465
inbox:
  enabled: true
  provider: gmail
  email: test@example.com
  password: shared-password
`

func TestEmailForProfileFallsBackToSharedBlock(t *testing.T) {
	cfg, err := Load(writeTestConfig(t, multiProfileMailConfig))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	def, err := cfg.GetProfile("default")
	if err != nil {
		t.Fatalf("GetProfile(default): %v", err)
	}
	got := cfg.EmailForProfile(def)
	if got.From != "test@example.com" || got.SMTP.Host != "smtp.example.com" {
		t.Errorf("EmailForProfile(default) = %+v, want the shared block", got)
	}
}

func TestEmailForProfileUsesOverride(t *testing.T) {
	cfg, err := Load(writeTestConfig(t, multiProfileMailConfig))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	spouse, err := cfg.GetProfile("spouse")
	if err != nil {
		t.Fatalf("GetProfile(spouse): %v", err)
	}
	got := cfg.EmailForProfile(spouse)
	if got.From != "spouse@gmail.com" || got.SMTP.Username != "spouse@gmail.com" {
		t.Errorf("EmailForProfile(spouse) = %+v, want the profile's own override", got)
	}
}

func TestInboxForProfileFallsBackAndOverrides(t *testing.T) {
	cfg, err := Load(writeTestConfig(t, multiProfileMailConfig))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	def, _ := cfg.GetProfile("default")
	if got := cfg.InboxForProfile(def); got.Email != "test@example.com" {
		t.Errorf("InboxForProfile(default).Email = %q, want the shared inbox", got.Email)
	}

	spouse, _ := cfg.GetProfile("spouse")
	if got := cfg.InboxForProfile(spouse); got.Email != "spouse@gmail.com" {
		t.Errorf("InboxForProfile(spouse).Email = %q, want the profile's own override", got.Email)
	}
}

func TestConfiguredInboxesDedupesSharedInbox(t *testing.T) {
	// Neither profile overrides inbox - both resolve to the same shared
	// inbox, which should only be scanned once.
	body := `
profiles:
  - id: default
    first_name: Test
    last_name: User
    email: test@example.com
  - id: spouse
    first_name: Spouse
    last_name: User
    email: spouse@example.com
email:
  provider: smtp
  from: test@example.com
  smtp:
    host: smtp.example.com
    port: 465
inbox:
  enabled: true
  provider: gmail
  email: test@example.com
  password: shared-password
`
	cfg, err := Load(writeTestConfig(t, body))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	inboxes := cfg.ConfiguredInboxes()
	if len(inboxes) != 1 {
		t.Fatalf("ConfiguredInboxes() returned %d inboxes, want 1 (deduped): %+v", len(inboxes), inboxes)
	}
}

func TestConfiguredInboxesIncludesDistinctOverrides(t *testing.T) {
	cfg, err := Load(writeTestConfig(t, multiProfileMailConfig))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	inboxes := cfg.ConfiguredInboxes()
	if len(inboxes) != 2 {
		t.Fatalf("ConfiguredInboxes() returned %d inboxes, want 2 (shared + spouse's override): %+v", len(inboxes), inboxes)
	}
}

func TestValidateRejectsInvalidProfileMailOverride(t *testing.T) {
	body := `
profiles:
  - id: default
    first_name: Test
    last_name: User
    email: test@example.com
  - id: spouse
    first_name: Spouse
    last_name: User
    email: spouse@example.com
    mail:
      email:
        provider: smtp
        from: spouse@gmail.com
email:
  provider: smtp
  from: test@example.com
  smtp:
    host: smtp.example.com
    port: 465
`
	cfg, err := Load(writeTestConfig(t, body))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() = nil, want an error for the spouse profile's incomplete mail.email override (missing smtp host/port)")
	}
}
