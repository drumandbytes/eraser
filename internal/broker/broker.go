package broker

import (
	"fmt"
	"net/url"
	"os"
	"strings"

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

// Priority values, in descending order of importance. Also the display
// order used by the CLI and the web UI's priority selector - don't derive
// that list from the values present in the database (the way categories and
// regions are derived), since priority has a meaningful rank order.
const (
	PriorityHigh   = "high"
	PriorityMedium = "medium"
	PriorityLow    = "low"
)

var Priorities = []string{PriorityHigh, PriorityMedium, PriorityLow}

// NormalizePriority lowercases/trims a priority value and returns "" for
// anything that isn't one of Priorities, so a typo in the YAML or a config
// file degrades to "unclassified" instead of silently matching nothing.
func NormalizePriority(p string) string {
	switch strings.ToLower(strings.TrimSpace(p)) {
	case PriorityHigh:
		return PriorityHigh
	case PriorityMedium:
		return PriorityMedium
	case PriorityLow:
		return PriorityLow
	default:
		return ""
	}
}

// PriorityRank orders priorities high < medium < low < unclassified, for
// sorting. Filtering doesn't use it - a priority filter is an exact match,
// not a "this and above" threshold.
func PriorityRank(p string) int {
	switch NormalizePriority(p) {
	case PriorityHigh:
		return 0
	case PriorityMedium:
		return 1
	case PriorityLow:
		return 2
	default:
		return 3
	}
}

func sanitizeBroker(b *Broker) {
	b.Priority = NormalizePriority(b.Priority)

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
	// Priority is how much this broker matters to a person trying to get
	// removed: "high", "medium" or "low" (see Priorities). It's a filter,
	// not a schedule - nothing sends automatically based on it. An empty
	// or unrecognized value means "unclassified" and is only matched by a
	// blank (all-priorities) filter. See docs/broker-priority.md for how
	// the shipped values were derived.
	Priority   string   `yaml:"priority,omitempty"`
	Notes      string   `yaml:"notes,omitempty"`
	RequiresID bool     `yaml:"requires_id,omitempty"` // If they require ID verification
	Tags       []string `yaml:"tags,omitempty"`
}

type BrokerDatabase struct {
	Brokers []Broker `yaml:"brokers"`
}

func LoadFromFile(path string) (*BrokerDatabase, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read broker file: %w", err)
	}

	var db BrokerDatabase
	if err := yaml.Unmarshal(data, &db); err != nil {
		return nil, fmt.Errorf("failed to parse broker file: %w", err)
	}

	for i := range db.Brokers {
		sanitizeBroker(&db.Brokers[i])
	}
	return &db, nil
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
