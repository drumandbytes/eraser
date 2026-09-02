package broker

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/eraser-privacy/eraser/data"
	"gopkg.in/yaml.v3"
)

func isValidURL(rawURL string) bool {
	if rawURL == "" {
		return true
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	scheme := strings.ToLower(u.Scheme)
	return scheme == "http" || scheme == "https"
}

func sanitizeBroker(b *Broker) {
	if !isValidURL(b.OptOutURL) {
		b.OptOutURL = ""
	}
	if !isValidURL(b.Website) {
		b.Website = ""
	}
}

type Broker struct {
	ID        string `yaml:"id"`
	Name      string `yaml:"name"`
	Email     string `yaml:"email"`
	Website   string `yaml:"website,omitempty"`
	OptOutURL string `yaml:"opt_out_url,omitempty"`
	Region    string `yaml:"region"`             // "us", "eu", "global"
	Category  string `yaml:"category,omitempty"` // "people-search", "marketing", "background-check", etc.
	Notes     string `yaml:"notes,omitempty"`
}

type BrokerDatabase struct {
	Brokers []Broker `yaml:"brokers"`
}

// Parse unmarshals and sanitizes a broker YAML document. Used to validate a
// freshly downloaded list before it replaces the local copy.
func Parse(raw []byte) (*BrokerDatabase, error) {
	var db BrokerDatabase
	if err := yaml.Unmarshal(raw, &db); err != nil {
		return nil, fmt.Errorf("failed to parse broker data: %w", err)
	}
	for i := range db.Brokers {
		sanitizeBroker(&db.Brokers[i])
	}
	return &db, nil
}

// LoadFromFile parses a broker YAML file from an explicit path.
func LoadFromFile(path string) (*BrokerDatabase, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read broker file: %w", err)
	}
	return Parse(raw)
}

// UserBrokersPath is where `eraser update-brokers` writes a refreshed copy of
// the list; Load() prefers it over the embedded default when it exists.
func UserBrokersPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".eraser", "brokers.yaml")
}

// Load resolves the broker database from, in order: overridePath (the
// --brokers flag) when set, then ~/.eraser/brokers.yaml when it exists, then
// the copy embedded in the binary. There is no implicit ./data or
// <exe-dir>/data scanning any more.
func Load(overridePath string) (*BrokerDatabase, error) {
	if overridePath != "" {
		return LoadFromFile(overridePath)
	}
	if p := UserBrokersPath(); p != "" {
		if _, err := os.Stat(p); err == nil {
			return LoadFromFile(p)
		}
	}
	return Parse(data.BrokersYAML)
}

func toSet(items []string) map[string]bool {
	m := make(map[string]bool, len(items))
	for _, s := range items {
		m[strings.ToLower(s)] = true
	}
	return m
}

// Filter returns brokers matching the given regions (empty means "all"),
// excluding any broker whose ID/name is in excluded or whose category
// (case-insensitive) is in excludedCategories - e.g. "requires-id" to skip
// brokers that demand a government ID document before acting on a request.
func (db *BrokerDatabase) Filter(regions []string, excluded []string, excludedCategories []string) []Broker {
	regionSet, excludedSet, excludedCatSet := toSet(regions), toSet(excluded), toSet(excludedCategories)

	var result []Broker
	for _, b := range db.Brokers {
		if excludedSet[strings.ToLower(b.ID)] || excludedSet[strings.ToLower(b.Name)] {
			continue
		}
		if excludedCatSet[strings.ToLower(b.Category)] {
			continue
		}
		if len(regionSet) > 0 {
			r := strings.ToLower(b.Region)
			if !regionSet[r] && !regionSet["global"] && r != "global" {
				continue
			}
		}
		result = append(result, b)
	}
	return result
}

func (db *BrokerDatabase) FindByID(id string) *Broker {
	id = strings.ToLower(id)
	for i := range db.Brokers {
		if strings.ToLower(db.Brokers[i].ID) == id {
			return &db.Brokers[i]
		}
	}
	return nil
}

func (db *BrokerDatabase) Save(path string) error {
	data, err := yaml.Marshal(db)
	if err != nil {
		return fmt.Errorf("failed to serialize brokers: %w", err)
	}
	return os.WriteFile(path, data, 0644)
}

func (db *BrokerDatabase) Add(broker Broker) error {
	if db.FindByID(broker.ID) != nil {
		return fmt.Errorf("broker with ID %q already exists", broker.ID)
	}
	db.Brokers = append(db.Brokers, broker)
	return nil
}

// FindByEmail finds a broker by their email address
func (db *BrokerDatabase) FindByEmail(email string) *Broker {
	email = strings.ToLower(email)
	for i := range db.Brokers {
		if strings.ToLower(db.Brokers[i].Email) == email {
			return &db.Brokers[i]
		}
	}
	return nil
}

// RemoveByEmail removes a broker by their email address
// Returns the removed broker, or nil if not found
func (db *BrokerDatabase) RemoveByEmail(email string) *Broker {
	email = strings.ToLower(email)
	for i := range db.Brokers {
		if strings.ToLower(db.Brokers[i].Email) == email {
			removed := db.Brokers[i]
			db.Brokers = append(db.Brokers[:i], db.Brokers[i+1:]...)
			return &removed
		}
	}
	return nil
}

// MarkEmailUnreachable finds a broker by their current email address and
// clears it, recording the old address and the reason in Notes, instead of
// removing the broker entirely. A bounced email usually means the company
// changed its privacy-request address, not that it stopped existing -
// deleting the whole record (name, category, website, opt-out URL) on a
// bounce throws away everything needed to give it a working address later.
// Returns the mutated broker, or nil if no broker has that email.
func (db *BrokerDatabase) MarkEmailUnreachable(email, reason string) *Broker {
	b := db.FindByEmail(email)
	if b == nil {
		return nil
	}

	oldEmail := b.Email
	b.Email = ""

	note := fmt.Sprintf("Email %s became undeliverable (%s, %s) - cleared, needs a working address.",
		oldEmail, reason, time.Now().Format("2006-01-02"))
	if b.Notes != "" {
		b.Notes = b.Notes + " " + note
	} else {
		b.Notes = note
	}

	return b
}

// RemoveByID removes a broker by their ID
// Returns the removed broker, or nil if not found
func (db *BrokerDatabase) RemoveByID(id string) *Broker {
	id = strings.ToLower(id)
	for i := range db.Brokers {
		if strings.ToLower(db.Brokers[i].ID) == id {
			removed := db.Brokers[i]
			db.Brokers = append(db.Brokers[:i], db.Brokers[i+1:]...)
			return &removed
		}
	}
	return nil
}

// SaveWithBackup saves the database to file, creating a backup first
func (db *BrokerDatabase) SaveWithBackup(path string) error {
	// Create backup
	if _, err := os.Stat(path); err == nil {
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("failed to read file for backup: %w", err)
		}
		backupPath := path + ".bak"
		if err := os.WriteFile(backupPath, data, 0644); err != nil {
			return fmt.Errorf("failed to create backup: %w", err)
		}
	}

	return db.Save(path)
}
