package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestProfileHelpers(t *testing.T) {
	if got := (Profile{FirstName: "Jane", LastName: "Doe"}).FullName(); got != "Jane Doe" {
		t.Errorf("FullName = %q", got)
	}
	if got := (Profile{FirstName: "Jane", MiddleName: "Q", LastName: "Doe"}).FullName(); got != "Jane Q Doe" {
		t.Errorf("FullName with middle = %q", got)
	}

	empty := &Config{}
	if empty.HasProfile() {
		t.Error("an empty config has no profile")
	}
	cfg := &Config{Profiles: []NamedProfile{
		{ID: "spouse", Profile: Profile{FirstName: "John"}},
		{ID: "default", Profile: Profile{FirstName: "Jane"}},
	}}
	if !cfg.HasProfile() || cfg.PrimaryProfile().ID != "default" {
		t.Errorf("HasProfile/PrimaryProfile = %v / %q", cfg.HasProfile(), cfg.PrimaryProfile().ID)
	}
	if p := (&Config{Profiles: []NamedProfile{{ID: "a"}, {ID: "b"}}}).PrimaryProfile(); p.ID != "a" {
		t.Errorf("PrimaryProfile without a default = %q, want the first", p.ID)
	}

	if _, err := cfg.GetProfile(""); err == nil || !strings.Contains(err.Error(), "spouse, default") {
		t.Errorf("ambiguous GetProfile: %v", err)
	}
	if p, err := cfg.GetProfile("SPOUSE"); err != nil || p.FirstName != "John" {
		t.Errorf("GetProfile case-insensitive = %+v, %v", p, err)
	}
	if _, err := cfg.GetProfile("ghost"); err == nil || !strings.Contains(err.Error(), `no profile "ghost"`) {
		t.Errorf("unknown GetProfile: %v", err)
	}
	if p, err := empty.GetProfile(""); err != nil || p.ID != DefaultProfileID {
		t.Errorf("single legacy profile = %+v, %v", p, err)
	}
}

func TestValidate(t *testing.T) {
	good := func() *Config {
		return &Config{
			Profiles: []NamedProfile{{ID: "jane", Profile: Profile{FirstName: "Jane", LastName: "Doe", Email: "jane@example.com"}}},
			Email:    EmailConfig{From: "jane@example.com", SMTP: SMTPConfig{Host: "smtp.example.com", Port: 465, Username: "jane"}},
		}
	}
	if err := good().Validate(); err != nil {
		t.Fatalf("valid config: %v", err)
	}
	noTLS := false
	cases := map[string]struct {
		mutate func(*Config)
		want   string
	}{
		"no id":          {func(c *Config) { c.Profiles[0].ID = "" }, "needs an id"},
		"duplicate id":   {func(c *Config) { c.Profiles = append(c.Profiles, c.Profiles[0]); c.Profiles[1].ID = "JANE" }, "duplicate profile id"},
		"no last name":   {func(c *Config) { c.Profiles[0].LastName = "" }, "first_name and last_name"},
		"no email":       {func(c *Config) { c.Profiles[0].Email = "" }, "email is required"},
		"bad provider":   {func(c *Config) { c.Email.Provider = "sendgrid" }, "unknown provider"},
		"no host":        {func(c *Config) { c.Email.SMTP.Host = "" }, "host is required"},
		"no from":        {func(c *Config) { c.Email.From = "" }, "from address"},
		"no port":        {func(c *Config) { c.Email.SMTP.Port = 0 }, "port is required"},
		"plaintext auth": {func(c *Config) { c.Email.SMTP.UseTLS = &noTLS }, "use_tls: false"},
	}
	for name, c := range cases {
		cfg := good()
		c.mutate(cfg)
		if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v, want %q", name, err, c.want)
		}
	}

	manual := good()
	manual.Email = EmailConfig{}
	manual.Options.SendMode = "Manual"
	if err := manual.Validate(); err != nil {
		t.Errorf("manual mode needs no email: %v", err)
	}
}

func TestValidateInbox(t *testing.T) {
	cases := []struct {
		inbox InboxConfig
		want  string
	}{
		{InboxConfig{}, "not enabled"},
		{InboxConfig{Enabled: true}, "email address is required"},
		{InboxConfig{Enabled: true, Email: "a@b.example"}, "password"},
		{InboxConfig{Enabled: true, Email: "a@b.example", Password: "p"}, "server is required"},
		{InboxConfig{Enabled: true, Email: "a@b.example", Password: "p", Server: "imap.b.example"}, "port is required"},
		{InboxConfig{Enabled: true, Email: "a@b.example", Password: "p", Server: "imap.b.example", Port: 993}, ""},
	}
	for _, c := range cases {
		err := (&Config{Inbox: c.inbox}).ValidateInbox()
		if c.want == "" && err != nil || c.want != "" && (err == nil || !strings.Contains(err.Error(), c.want)) {
			t.Errorf("%+v: %v, want %q", c.inbox, err, c.want)
		}
	}
}

func TestApplyInboxDefaults(t *testing.T) {
	in := InboxConfig{Provider: "fastmail"}
	ApplyInboxDefaults(&in)
	if in.Server != "imap.fastmail.com" || in.Port != 993 || in.Folder != "INBOX" || in.ArchiveFolder != "Eraser" {
		t.Errorf("fastmail defaults = %+v", in)
	}
	legacy := InboxConfig{Provider: "outlook"}
	ApplyInboxDefaults(&legacy)
	if legacy.Server != "outlook.office365.com" {
		t.Errorf("legacy outlook = %+v", legacy)
	}
	kept := InboxConfig{Provider: "fastmail", Server: "imap.custom.example", Port: 143}
	ApplyInboxDefaults(&kept)
	if kept.Server != "imap.custom.example" || kept.Port != 143 {
		t.Errorf("explicit server overwritten: %+v", kept)
	}
}

func TestProviderLookups(t *testing.T) {
	for _, c := range []struct{ smtp, imap, want string }{
		{"", "", "gmail"},
		{"smtp.fastmail.com", "", "fastmail"},
		{"SMTP.GMAIL.COM", "", "gmail"},
		{"", "imap.mail.me.com", "icloud"},
		{"email-smtp.us-east-1.amazonaws.com", "", "ses"},
		{"mail.example.org", "", "custom"},
	} {
		if got := ProviderIDForHosts(c.smtp, c.imap); got != c.want {
			t.Errorf("ProviderIDForHosts(%q, %q) = %q, want %q", c.smtp, c.imap, got, c.want)
		}
	}
	if got := ProviderName("smtp.fastmail.com"); got != "Fastmail" {
		t.Errorf("ProviderName = %q", got)
	}
	if got := ProviderName("mail.example.org"); got != "mail.example.org" {
		t.Errorf("unknown host name = %q", got)
	}
	if got := ProviderName(""); got != "" {
		t.Errorf("empty host name = %q", got)
	}
}

func TestTLSHelpers(t *testing.T) {
	if !ImplicitTLS(465) || !ImplicitTLS(993) || ImplicitTLS(587) || ImplicitTLS(143) {
		t.Error("ImplicitTLS wrong")
	}
	for host, skip := range map[string]bool{"127.0.0.1": true, "::1": true, "LocalHost": true, "smtp.gmail.com": false, "10.0.0.1": false} {
		if got := TLSFor(host).InsecureSkipVerify; got != skip {
			t.Errorf("TLSFor(%q).InsecureSkipVerify = %v", host, got)
		}
	}
}

func TestDefaultConfigPathAndPermissions(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if got := DefaultConfigPath(); got != filepath.Join(home, ".eraser", "config.yaml") {
		t.Errorf("DefaultConfigPath = %q", got)
	}

	path := filepath.Join(home, "c.yaml")
	if err := Save(path, &Config{Profiles: []NamedProfile{{ID: "a", Profile: Profile{FirstName: "A"}}}}); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(path, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := checkFilePermissions(path); err == nil {
			t.Error("0644 config not flagged")
		}
		if _, err := Load(path); err != nil { // a warning, not a failure
			t.Errorf("Load of a 0644 config: %v", err)
		}
	}
	if err := checkFilePermissions(filepath.Join(home, "missing.yaml")); err == nil {
		t.Error("missing file not reported")
	}
}

func TestLoadAndSaveErrors(t *testing.T) {
	dir := t.TempDir()
	if _, err := Load(filepath.Join(dir, "missing.yaml")); err == nil {
		t.Error("Load of a missing file succeeded")
	}
	bad := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(bad, []byte("profiles: [oops"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(bad); err == nil || !strings.Contains(err.Error(), "parse") {
		t.Errorf("Load of bad YAML: %v", err)
	}
	blocker := filepath.Join(dir, "file")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Save(filepath.Join(blocker, "config.yaml"), &Config{}); err == nil {
		t.Error("Save under a file succeeded")
	}
}
