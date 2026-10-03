package config

import (
	"os"
	"path/filepath"
	"strings"
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

func TestLoadFillsInboxServerFromProviderPreset(t *testing.T) {
	path := writeTestConfig(t, minimalConfig+"inbox:\n  enabled: true\n  provider: proton\n  email: test@example.com\n  password: x\n")
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Inbox.Server != "127.0.0.1" || cfg.Inbox.Port != 1143 {
		t.Errorf("proton inbox = %s:%d, want 127.0.0.1:1143", cfg.Inbox.Server, cfg.Inbox.Port)
	}
}

// A pre-0.10 config: legacy profile: block, provider: smtp, explicit use_tls,
// inbox provider without server, and since-removed pipeline keys.
const legacyConfig = `
profile:
  first_name: Jane
  last_name: Doe
  email: jane@example.org
email:
  provider: smtp
  from: jane@example.org
  smtp:
    host: smtp.gmail.com
    port: 465
    username: jane@example.org
    password: x
    use_tls: true
options:
  template: gdpr
  dry_run: false
  regions: []
inbox:
  enabled: true
  provider: gmail
  email: jane@example.org
  password: x
pipeline:
  auto_confirm: false
  auto_fill_forms: false
  browser_timeout_sec: 30
`

func TestLegacyConfigLoadsAndSavesInCurrentFormat(t *testing.T) {
	cfg, err := Load(writeTestConfig(t, legacyConfig))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if p := cfg.PrimaryProfile(); p.ID != DefaultProfileID || p.FirstName != "Jane" || len(cfg.Profiles) != 1 {
		t.Fatalf("legacy profile: not moved into profiles: %+v", cfg.Profiles)
	}
	if cfg.Inbox.Server != "imap.gmail.com" || !cfg.Email.SMTP.TLS() {
		t.Fatalf("inbox/tls defaults lost: %+v %+v", cfg.Inbox, cfg.Email.SMTP)
	}

	path := filepath.Join(t.TempDir(), "saved.yaml")
	if err := Save(path, cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}
	raw, _ := os.ReadFile(path)
	for _, gone := range []string{"\nprofile:", "auto_confirm", "auto_fill_forms", "dry_run", "regions"} {
		if strings.Contains(string(raw), gone) {
			t.Errorf("saved config still has %q:\n%s", gone, raw)
		}
	}
	again, err := Load(path)
	if err != nil || again.PrimaryProfile().Email != "jane@example.org" || again.Email.SMTP.Host != "smtp.gmail.com" {
		t.Fatalf("round trip lost data: %v %+v", err, again)
	}
}

// The minimal current-format config: no provider:, no use_tls.
func TestMinimalEmailConfigDefaultsToTLS(t *testing.T) {
	e := EmailConfig{From: "a@example.org", SMTP: SMTPConfig{Host: "smtp.example.org", Port: 587, Username: "a", Password: "p"}}
	if err := validateEmailConfig(e); err != nil || !e.SMTP.TLS() {
		t.Fatalf("validate = %v, TLS = %v", err, e.SMTP.TLS())
	}
	e.SMTP.UseTLS = new(false)
	if err := validateEmailConfig(e); err == nil {
		t.Fatal("use_tls: false with a username must be rejected")
	}
	e.Provider = "sendgrid"
	if err := validateEmailConfig(e); err == nil || !strings.Contains(err.Error(), "only smtp") {
		t.Fatalf("unknown provider: %v", err)
	}
}

func TestValidateInboxNamesUnknownProvider(t *testing.T) {
	err := validateInboxConfig(InboxConfig{Enabled: true, Provider: "outlok", Email: "a@b.c", Password: "p", Port: 993})
	if err == nil || !strings.Contains(err.Error(), `"outlok"`) {
		t.Fatalf("got %v", err)
	}
}
