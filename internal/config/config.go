package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"gopkg.in/yaml.v3"
)

const defaultRateLimitMs = 2000

// defaultDailySendLimit stays safely under Gmail's ~500/day cap for a
// regular (non-Workspace) account, leaving headroom for other mail you send
// that same day.
const defaultDailySendLimit = 450

func checkFilePermissions(path string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if perm := info.Mode().Perm(); perm&0077 != 0 {
		return fmt.Errorf("config file %s has insecure permissions %04o; should be 0600", path, perm)
	}
	return nil
}

type Config struct {
	Profile  Profile     `yaml:"profile"`
	Email    EmailConfig `yaml:"email"`
	Options  Options     `yaml:"options"`
	Inbox    InboxConfig `yaml:"inbox,omitempty"`
	Pipeline Pipeline    `yaml:"pipeline,omitempty"`
}

// InboxConfig holds IMAP settings for monitoring broker responses
type InboxConfig struct {
	Enabled       bool   `yaml:"enabled"`
	Provider      string `yaml:"provider"`       // "gmail", "outlook", "imap"
	Server        string `yaml:"server"`         // e.g., "imap.gmail.com"
	Port          int    `yaml:"port"`           // e.g., 993
	Email         string `yaml:"email"`          // Email address to monitor
	Password      string `yaml:"password"`       // App password (not main password)
	Folder        string `yaml:"folder"`         // Folder to monitor (default: "INBOX")
	AutoArchive   bool   `yaml:"auto_archive"`   // Automatically move processed emails to archive folder
	ArchiveFolder string `yaml:"archive_folder"` // Folder to archive emails to (default: "Eraser")
}

// Pipeline holds settings for the automation pipeline
type Pipeline struct {
	AutoConfirm   bool `yaml:"auto_confirm"`    // Auto-click confirmation links
	AutoFillForms bool `yaml:"auto_fill_forms"` // Enable browser automation for forms
	// BrowserHeadless defaults to true (headless) when unset. A plain bool
	// can't tell "explicitly set to false" apart from "never set" - both
	// unmarshal as the zero value - so this used to get silently forced
	// back to true on every load, discarding a `browser_headless: false`
	// someone set to watch the browser solve a CAPTCHA. A pointer fixes
	// that; use Headless() to read it with the default applied.
	BrowserHeadless   *bool `yaml:"browser_headless,omitempty"`
	BrowserTimeoutSec int   `yaml:"browser_timeout_sec"` // Browser operation timeout
}

// Headless returns the effective headless setting, defaulting to true when
// BrowserHeadless was never set in the config.
func (p Pipeline) Headless() bool {
	if p.BrowserHeadless == nil {
		return true
	}
	return *p.BrowserHeadless
}

type Profile struct {
	FirstName string `yaml:"first_name"`
	LastName  string `yaml:"last_name"`
	Email     string `yaml:"email"`
	// AdditionalEmails are other addresses you've used over the years (old
	// personal accounts, work emails, etc). Brokers often indexed your record
	// under one of these rather than your current address, so listing them
	// all in the removal request helps them actually locate and delete it.
	AdditionalEmails []string `yaml:"additional_emails,omitempty"`
	// NameVariants covers other spellings brokers may have indexed you under -
	// e.g. a diacritic-free version of your name ("Maris" for "Māris"), a
	// maiden name, or a nickname you've used to sign up for things.
	NameVariants []string `yaml:"name_variants,omitempty"`
	// PreviousAddresses are other places you've lived recently enough that a
	// broker might still have the record - a partial address (missing an
	// apartment number, say) is still worth including, since most broker
	// matching keys off street/city/postal code rather than the exact unit.
	PreviousAddresses []string `yaml:"previous_addresses,omitempty"`
	Address           string   `yaml:"address,omitempty"`
	City              string   `yaml:"city,omitempty"`
	State             string   `yaml:"state,omitempty"`
	ZipCode           string   `yaml:"zip_code,omitempty"`
	Country           string   `yaml:"country,omitempty"`
	Phone             string   `yaml:"phone,omitempty"`
	// AdditionalPhones covers other numbers you've used to sign up for
	// things (an old number, a work line) that a broker might have on file
	// instead of your current one.
	AdditionalPhones []string `yaml:"additional_phones,omitempty"`
	DateOfBirth      string   `yaml:"date_of_birth,omitempty"`
}

func (p Profile) FullName() string { return p.FirstName + " " + p.LastName }

type EmailConfig struct {
	Provider string     `yaml:"provider"`
	From     string     `yaml:"from"`
	SMTP     SMTPConfig `yaml:"smtp,omitempty"`
}

type Email = EmailConfig

type SMTPConfig struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
	UseTLS   bool   `yaml:"use_tls"`
}

type Options struct {
	Template    string `yaml:"template"`
	DryRun      bool   `yaml:"dry_run"`
	RateLimitMs int    `yaml:"rate_limit_ms"`
	// DailySendLimit caps how many emails `send` will dispatch per rolling
	// 24h window, so a large broker list can't blow past your provider's
	// daily sending cap (Gmail's is ~500/day) or read as bulk-spam behavior.
	// 0 uses the default (450). Overridden per-run with --ignore-daily-limit.
	DailySendLimit  int      `yaml:"daily_send_limit,omitempty"`
	Regions         []string `yaml:"regions"`
	ExcludedBrokers []string `yaml:"excluded_brokers,omitempty"`
}

func DefaultConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "config.yaml"
	}
	return filepath.Join(home, ".eraser", "config.yaml")
}

func Load(path string) (*Config, error) {
	if err := checkFilePermissions(path); err != nil {
		fmt.Fprintf(os.Stderr, "WARNING: %v\n", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	if cfg.Options.Template == "" {
		cfg.Options.Template = "generic"
	}
	if cfg.Options.RateLimitMs == 0 {
		cfg.Options.RateLimitMs = defaultRateLimitMs
	}
	if cfg.Options.DailySendLimit == 0 {
		cfg.Options.DailySendLimit = defaultDailySendLimit
	}

	// Set inbox defaults
	if cfg.Inbox.Folder == "" {
		cfg.Inbox.Folder = "INBOX"
	}
	if cfg.Inbox.ArchiveFolder == "" {
		cfg.Inbox.ArchiveFolder = "Eraser"
	}
	if cfg.Inbox.Provider == "gmail" && cfg.Inbox.Server == "" {
		cfg.Inbox.Server = "imap.gmail.com"
		cfg.Inbox.Port = 993
	}
	if cfg.Inbox.Provider == "outlook" && cfg.Inbox.Server == "" {
		cfg.Inbox.Server = "outlook.office365.com"
		cfg.Inbox.Port = 993
	}

	// Set pipeline defaults
	if cfg.Pipeline.BrowserTimeoutSec == 0 {
		cfg.Pipeline.BrowserTimeoutSec = 30
	}
	// BrowserHeadless is intentionally left as-is here (nil if unset) -
	// see Pipeline.Headless(), which applies the true default without
	// clobbering an explicit false.

	return &cfg, nil
}

func Save(path string, cfg *Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("failed to serialize config: %w", err)
	}
	return os.WriteFile(path, data, 0600)
}

func (c *Config) Validate() error {
	if c.Profile.FirstName == "" || c.Profile.LastName == "" {
		return fmt.Errorf("profile: first_name and last_name are required")
	}
	if c.Profile.Email == "" {
		return fmt.Errorf("profile: email is required")
	}
	if c.Email.Provider == "" {
		return fmt.Errorf("email: provider is required")
	}
	if c.Email.From == "" {
		return fmt.Errorf("email: from address is required")
	}

	if c.Email.Provider != "smtp" {
		return fmt.Errorf("email: unknown provider %q (only smtp is supported)", c.Email.Provider)
	}
	if c.Email.SMTP.Host == "" {
		return fmt.Errorf("email.smtp: host is required")
	}
	if c.Email.SMTP.Port == 0 {
		return fmt.Errorf("email.smtp: port is required")
	}

	return nil
}

// ValidateInbox validates inbox configuration (only called when inbox monitoring is used)
func (c *Config) ValidateInbox() error {
	if !c.Inbox.Enabled {
		return fmt.Errorf("inbox: monitoring is not enabled in config")
	}
	if c.Inbox.Email == "" {
		return fmt.Errorf("inbox: email address is required")
	}
	if c.Inbox.Password == "" {
		return fmt.Errorf("inbox: password (app password) is required")
	}
	if c.Inbox.Server == "" {
		return fmt.Errorf("inbox: IMAP server is required")
	}
	if c.Inbox.Port == 0 {
		return fmt.Errorf("inbox: IMAP port is required")
	}
	return nil
}
