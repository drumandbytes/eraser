package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

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
	// Profile is the legacy single-profile block, wrapped by GetProfiles as
	// "default". Ignored once Profiles is non-empty.
	Profile Profile `yaml:"profile"`
	// Profiles tracks removal requests for several people against the same
	// brokers, mail account and options; history is kept per profile
	// (profile_id). Takes precedence over Profile.
	Profiles []NamedProfile `yaml:"profiles,omitempty"`
	Email    EmailConfig    `yaml:"email"`
	Options  Options        `yaml:"options"`
	Inbox    InboxConfig    `yaml:"inbox,omitempty"`
	Pipeline Pipeline       `yaml:"pipeline,omitempty"`
	Schedule Schedule       `yaml:"schedule,omitempty"`
}

// Schedule is the in-app fallback for automated cycles when the OS scheduler
// ('eraser schedule install') isn't set up.
type Schedule struct {
	// Enabled makes a running 'eraser serve' run a cycle every 6 hours.
	// Ignored while the OS job is installed.
	Enabled bool `yaml:"enabled,omitempty"`
}

// NamedProfile is a person's identity plus the stable ID used by --profile,
// history rows and the web UI's switcher.
type NamedProfile struct {
	// ID: short, lowercase, hyphenated. Stored verbatim in history.db, so
	// renaming it orphans the profile's existing history.
	ID      string `yaml:"id"`
	Profile `yaml:",inline"`
	// Mail overrides the shared email:/inbox: blocks for this profile only
	// (see EmailForProfile/InboxForProfile). nil = use the shared blocks.
	Mail *MailConfig `yaml:"mail,omitempty"`
}

// MailConfig holds one profile's email/inbox overrides. Either field may be
// set independently - e.g. a profile can send from its own SMTP account
// while still sharing the default inbox for reply monitoring.
type MailConfig struct {
	Email *EmailConfig `yaml:"email,omitempty"`
	Inbox *InboxConfig `yaml:"inbox,omitempty"`
}

// EmailForProfile returns the SMTP config to use for sends made under this
// profile: its Mail.Email override if set, otherwise the shared top-level
// Email block.
func (c *Config) EmailForProfile(p NamedProfile) EmailConfig {
	if p.Mail != nil && p.Mail.Email != nil {
		return *p.Mail.Email
	}
	return c.Email
}

// InboxForProfile returns the IMAP config to use when monitoring replies for
// this profile: its Mail.Inbox override if set, otherwise the shared
// top-level Inbox block.
func (c *Config) InboxForProfile(p NamedProfile) InboxConfig {
	if p.Mail != nil && p.Mail.Inbox != nil {
		return *p.Mail.Inbox
	}
	return c.Inbox
}

// ConfiguredInboxes returns every enabled IMAP inbox across profiles (overrides
// or the shared one), deduplicated by address, so `monitor` covers everyone.
func (c *Config) ConfiguredInboxes() []InboxConfig {
	seen := make(map[string]bool)
	var result []InboxConfig
	for _, p := range c.GetProfiles() {
		inbox := c.InboxForProfile(p)
		if !inbox.Enabled {
			continue
		}
		key := strings.ToLower(inbox.Email)
		if seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, inbox)
	}
	return result
}

// DefaultProfileID is the synthetic ID used for the legacy single Profile
// block when no profiles: list is configured, and is what pre-multi-profile
// history rows are attributed to after the profile_id migration.
const DefaultProfileID = "default"

// GetProfiles returns every profile; with no profiles: list, the legacy
// profile: block as a single "default" profile.
func (c *Config) GetProfiles() []NamedProfile {
	if len(c.Profiles) > 0 {
		return c.Profiles
	}
	return []NamedProfile{{ID: DefaultProfileID, Profile: c.Profile}}
}

// GetProfile resolves a profile by ID. Empty id works only when exactly one
// profile exists; otherwise it errors with the available IDs.
func (c *Config) GetProfile(id string) (NamedProfile, error) {
	profiles := c.GetProfiles()
	if id == "" {
		if len(profiles) == 1 {
			return profiles[0], nil
		}
		return NamedProfile{}, fmt.Errorf("multiple profiles configured (%s) - specify one with --profile", strings.Join(profileIDs(profiles), ", "))
	}
	for _, p := range profiles {
		if strings.EqualFold(p.ID, id) {
			return p, nil
		}
	}
	return NamedProfile{}, fmt.Errorf("no profile %q configured (available: %s)", id, strings.Join(profileIDs(profiles), ", "))
}

func profileIDs(profiles []NamedProfile) []string {
	ids := make([]string, len(profiles))
	for i, p := range profiles {
		ids[i] = p.ID
	}
	return ids
}

var nonSlugChars = regexp.MustCompile(`[^a-z0-9]+`)

// SlugifyID lowercases and hyphenates s, keeping only [a-z0-9]. Every profile
// ID must go through it: net/http silently drops non-ASCII bytes from
// Set-Cookie, so an ID with diacritics breaks the web UI's profile cookie.
func SlugifyID(s string) string {
	base := nonSlugChars.ReplaceAllString(strings.ToLower(strings.TrimSpace(s)), "-")
	base = strings.Trim(base, "-")
	if base == "" {
		return "profile"
	}
	return base
}

// SlugifyProfileID derives an ID from first/last name (see SlugifyID),
// appending -2, -3, ... when taken.
func SlugifyProfileID(firstName, lastName string, existing []NamedProfile) string {
	base := SlugifyID(firstName + "-" + lastName)

	taken := make(map[string]bool, len(existing))
	for _, p := range existing {
		taken[strings.ToLower(p.ID)] = true
	}

	id := base
	for n := 2; taken[id]; n++ {
		id = fmt.Sprintf("%s-%d", base, n)
	}
	return id
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
	// BrowserHeadless is a pointer so an explicit false survives loading (a
	// plain bool was forced back to true). Read it via Headless().
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
	// MiddleName: some brokers (background-check, financial-b2b) match on
	// full legal name.
	MiddleName string `yaml:"middle_name,omitempty"`
	LastName   string `yaml:"last_name"`
	Email      string `yaml:"email"`
	// AdditionalEmails: older addresses a broker may have indexed you under.
	AdditionalEmails []string `yaml:"additional_emails,omitempty"`
	// NameVariants covers other spellings brokers may have indexed you under -
	// e.g. a diacritic-free version of your name ("Maris" for "Māris"), a
	// maiden name, or a nickname you've used to sign up for things.
	NameVariants []string `yaml:"name_variants,omitempty"`
	// PreviousAddresses: partial ones still help, since matching keys off
	// street/city/postal code.
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

func (p Profile) FullName() string {
	if p.MiddleName == "" {
		return p.FirstName + " " + p.LastName
	}
	return p.FirstName + " " + p.MiddleName + " " + p.LastName
}

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
	Template string `yaml:"template"`
	// SendMode:
	//   "" / "smtp" - Eraser sends over the configured SMTP account.
	//   "manual"    - Eraser renders the emails for you to send yourself and
	//                 record with `eraser mark-sent` / "Mark sent". No email:
	//                 block needed; for users who won't share mailbox credentials.
	SendMode    string `yaml:"send_mode,omitempty"`
	DryRun      bool   `yaml:"dry_run"`
	RateLimitMs int    `yaml:"rate_limit_ms"`
	// DailySendLimit caps sends per rolling 24h to stay under provider limits
	// (Gmail ~500/day). 0 = 450. Bypass with --ignore-daily-limit.
	DailySendLimit int `yaml:"daily_send_limit,omitempty"`
	// BrokerList: "" / "full" (~750 entries, default) or "verified"
	// (data/brokers-verified.yaml). Overridden by `send --list`; ignored when
	// BrokerFile or --brokers is set.
	BrokerList string `yaml:"broker_list,omitempty"`
	// BrokerFile points the send-family commands at your own broker list,
	// the config equivalent of the global --brokers flag (which still wins).
	// Takes precedence over BrokerList.
	BrokerFile      string   `yaml:"broker_file,omitempty"`
	Regions         []string `yaml:"regions"`
	ExcludedBrokers []string `yaml:"excluded_brokers,omitempty"`
	// ExcludedCategories skips brokers by category (case-insensitive), e.g.
	// "requires-id" for brokers that demand an ID document.
	ExcludedCategories []string `yaml:"excluded_categories,omitempty"`
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
		// Fork default is gdpr, not upstream's generic - see EU-NOTES.md.
		// Only fills in a genuinely missing field; an explicit template in
		// config.yaml (which every real user of this fork will have) wins.
		cfg.Options.Template = "gdpr"
	}
	if cfg.Options.RateLimitMs == 0 {
		cfg.Options.RateLimitMs = defaultRateLimitMs
	}
	if cfg.Options.DailySendLimit == 0 {
		cfg.Options.DailySendLimit = defaultDailySendLimit
	}

	applyInboxDefaults(&cfg.Inbox)
	for i := range cfg.Profiles {
		if cfg.Profiles[i].Mail != nil && cfg.Profiles[i].Mail.Inbox != nil {
			applyInboxDefaults(cfg.Profiles[i].Mail.Inbox)
		}
	}

	if cfg.Pipeline.BrowserTimeoutSec == 0 {
		cfg.Pipeline.BrowserTimeoutSec = 30
	}
	// BrowserHeadless is intentionally left as-is here (nil if unset) -
	// see Pipeline.Headless(), which applies the true default without
	// clobbering an explicit false.

	return &cfg, nil
}

// applyInboxDefaults fills Folder/ArchiveFolder and, for known providers,
// Server/Port. Shared by the top-level inbox and per-profile overrides.
func applyInboxDefaults(inbox *InboxConfig) {
	if inbox.Folder == "" {
		inbox.Folder = "INBOX"
	}
	if inbox.ArchiveFolder == "" {
		inbox.ArchiveFolder = "Eraser"
	}
	if inbox.Provider == "gmail" && inbox.Server == "" {
		inbox.Server = "imap.gmail.com"
		inbox.Port = 993
	}
	if inbox.Provider == "outlook" && inbox.Server == "" {
		inbox.Server = "outlook.office365.com"
		inbox.Port = 993
	}
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
	profiles := c.GetProfiles()
	seen := make(map[string]bool, len(profiles))
	manual := c.IsManualSend()
	for _, np := range profiles {
		if np.ID == "" {
			return fmt.Errorf("profiles: every profile needs an id")
		}
		key := strings.ToLower(np.ID)
		if seen[key] {
			return fmt.Errorf("profiles: duplicate profile id %q", np.ID)
		}
		seen[key] = true
		if np.FirstName == "" || np.LastName == "" {
			return fmt.Errorf("profile %q: first_name and last_name are required", np.ID)
		}
		if np.Email == "" {
			return fmt.Errorf("profile %q: email is required", np.ID)
		}
		if !manual && np.Mail != nil && np.Mail.Email != nil {
			if err := validateEmailConfig(*np.Mail.Email); err != nil {
				return fmt.Errorf("profile %q: mail.email: %w", np.ID, err)
			}
		}
	}

	// Manual mode sends nothing itself, so it needs no email configuration.
	if manual {
		return nil
	}

	return validateEmailConfig(c.Email)
}

func validateEmailConfig(e EmailConfig) error {
	if e.Provider == "" {
		return fmt.Errorf("email: provider is required (or set options.send_mode: manual)")
	}
	if e.From == "" {
		return fmt.Errorf("email: from address is required")
	}
	if e.Provider != "smtp" {
		return fmt.Errorf("email: unknown provider %q (only smtp is supported)", e.Provider)
	}
	if e.SMTP.Host == "" {
		return fmt.Errorf("email.smtp: host is required")
	}
	if e.SMTP.Port == 0 {
		return fmt.Errorf("email.smtp: port is required")
	}
	return nil
}

// IsManualSend reports whether the user opted out of automated sending -
// Eraser renders the emails but never transmits them.
func (c *Config) IsManualSend() bool {
	return strings.EqualFold(c.Options.SendMode, "manual")
}

// ValidateInbox validates inbox configuration (only called when inbox monitoring is used)
func (c *Config) ValidateInbox() error {
	return validateInboxConfig(c.Inbox)
}

func validateInboxConfig(inbox InboxConfig) error {
	if !inbox.Enabled {
		return fmt.Errorf("inbox: monitoring is not enabled in config")
	}
	if inbox.Email == "" {
		return fmt.Errorf("inbox: email address is required")
	}
	if inbox.Password == "" {
		return fmt.Errorf("inbox: password (app password) is required")
	}
	if inbox.Server == "" {
		return fmt.Errorf("inbox: IMAP server is required")
	}
	if inbox.Port == 0 {
		return fmt.Errorf("inbox: IMAP port is required")
	}
	return nil
}
