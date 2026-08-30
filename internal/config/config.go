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
	// Profile is the legacy single-profile block. Still fully supported -
	// GetProfiles() wraps it as a single "default" profile whenever Profiles
	// is empty, so an existing single-person config.yaml keeps working
	// unchanged. Ignored once Profiles is non-empty (see GetProfiles).
	Profile Profile `yaml:"profile"`
	// Profiles lets one eraser install send and track removal requests for
	// more than one person (e.g. yourself and a family member) against the
	// same broker database, email account, and options - each profile's
	// send history, pipeline state, and pending tasks are tracked
	// separately (see history.Store's profile_id column). Takes precedence
	// over Profile when non-empty; Profile is otherwise ignored.
	Profiles []NamedProfile `yaml:"profiles,omitempty"`
	Email    EmailConfig    `yaml:"email"`
	Options  Options        `yaml:"options"`
	Inbox    InboxConfig    `yaml:"inbox,omitempty"`
	Pipeline Pipeline       `yaml:"pipeline,omitempty"`
}

// NamedProfile is one entry in a multi-profile config: a person identity
// (name, address, contact info - everything Profile already carries) plus a
// stable ID used to select it via --profile on the CLI, to scope history
// records to it, and to switch between profiles in the web UI.
type NamedProfile struct {
	// ID should be short, lowercase, and hyphenated (like a broker ID) -
	// e.g. "maris" or "spouse". Stored verbatim in history.db so changing it
	// later orphans that profile's existing history from the CLI/web UI
	// (the rows are still in the database, just no longer reachable by the
	// new ID).
	ID      string `yaml:"id"`
	Profile `yaml:",inline"`
}

// DefaultProfileID is the synthetic ID used for the legacy single Profile
// block when no profiles: list is configured, and is what pre-multi-profile
// history rows are attributed to after the profile_id migration.
const DefaultProfileID = "default"

// GetProfiles returns every configured profile. A config with no explicit
// profiles: list returns a single synthetic profile (ID "default") wrapping
// the legacy top-level profile: block, so existing single-profile configs
// keep working unchanged without needing to be rewritten.
func (c *Config) GetProfiles() []NamedProfile {
	if len(c.Profiles) > 0 {
		return c.Profiles
	}
	return []NamedProfile{{ID: DefaultProfileID, Profile: c.Profile}}
}

// GetProfile resolves the active profile by ID. An empty id resolves to the
// sole configured profile when there's exactly one; with several configured,
// an empty id is ambiguous and returns an error listing the available IDs
// (the caller - CLI flag, web session - must disambiguate).
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

// SlugifyID converts arbitrary text into a lowercase-hyphenated identifier
// safe to use as a NamedProfile.ID - stripping everything outside [a-z0-9].
// Both the CLI (`eraser profile add`) and the web UI's "add profile" form
// must funnel a user-typed or name-derived ID through this same function:
// Go's net/http cookie writer silently drops (rather than quotes) any byte
// outside 0x20-0x7e when writing a Set-Cookie header, including any
// non-ASCII UTF-8 byte such as diacritics in a name - an ID that carries
// one round-trips incorrectly through the web UI's profile-switcher
// cookie, silently falling back to the first configured profile.
func SlugifyID(s string) string {
	base := nonSlugChars.ReplaceAllString(strings.ToLower(strings.TrimSpace(s)), "-")
	base = strings.Trim(base, "-")
	if base == "" {
		return "profile"
	}
	return base
}

// SlugifyProfileID derives a profile ID from a first/last name pair (see
// SlugifyID for the charset rule), appending -2, -3, ... if the base is
// already taken by an existing profile - so the web UI's "add profile" form
// never needs to ask the user to pick an ID themselves.
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

// SplitAndTrimAny splits s on any single character appearing in seps, then
// trims each part and drops the empties - returning nil rather than an empty
// slice for blank input, so an unset value marshals away under `omitempty`.
//
// seps is a *set of characters*, not a separator string: passing ",\n" splits
// on a comma or a newline (either one), which is what a free-text field needs
// when the user may type one address per line, a comma-separated list, or a
// mix of both. Callers that need to split on a single character (the CLI's
// prompts) just pass that one character.
func SplitAndTrimAny(s, seps string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.FieldsFunc(s, func(r rune) bool {
		return strings.ContainsRune(seps, r)
	})
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			result = append(result, p)
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

// NormalizeAdditionalEmails cleans a parsed additional-address list for
// storage: it drops case-insensitive duplicates and any entry that merely
// repeats the primary address (which every template already prints on its own
// line, so listing it again just pads the request). Original casing of the
// first occurrence is kept - some brokers echo the address back verbatim, and
// the local part of an address is technically case-sensitive.
//
// It deliberately does no validity checking; callers validate before storing
// (see internal/email.ValidateEmail) so that an invalid entry can be reported
// against the field the user typed it into rather than silently dropped here.
func NormalizeAdditionalEmails(primary string, emails []string) []string {
	seen := make(map[string]bool, len(emails)+1)
	if primary = strings.TrimSpace(primary); primary != "" {
		seen[strings.ToLower(primary)] = true
	}

	result := make([]string, 0, len(emails))
	for _, e := range emails {
		e = strings.TrimSpace(e)
		key := strings.ToLower(e)
		if e == "" || seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, e)
	}
	if len(result) == 0 {
		return nil
	}
	return result
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
	// MiddleName is optional - most brokers only match on first/last name,
	// but some (especially background-check and financial-b2b brokers)
	// index records by full legal name, so including it helps them find
	// (and thus actually delete) the right record.
	MiddleName string `yaml:"middle_name,omitempty"`
	LastName   string `yaml:"last_name"`
	Email      string `yaml:"email"`
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
	// ExcludedCategories skips every broker whose category (case-insensitive)
	// matches one of these - e.g. "requires-id" to skip brokers that ask for
	// a government-issued ID document or similar heavyweight identity
	// verification before they'll act on a request, which this tool won't
	// supply on your behalf.
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
	profiles := c.GetProfiles()
	seen := make(map[string]bool, len(profiles))
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
