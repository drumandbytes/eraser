package broker

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// LocalPath holds brokers the user added or corrected themselves. It's kept
// apart from UserBrokersPath because update-brokers replaces that file
// wholesale - additions written there were silently lost on the next update.
// Entries are merged over whichever list is in use (except an explicit
// --brokers file); same id = the local entry wins.
func LocalPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".eraser", "brokers.local.yaml")
}

// LoadLocal returns the user's own entries; a missing file is an empty list.
func LoadLocal() (*BrokerDatabase, error) {
	p := LocalPath()
	if p == "" {
		return &BrokerDatabase{}, nil
	}
	if _, err := os.Stat(p); os.IsNotExist(err) {
		return &BrokerDatabase{}, nil
	}
	return LoadFromFile(p)
}

// SaveLocal adds b to the user's own entries, replacing one with the same id.
func SaveLocal(b Broker) error {
	local, err := LoadLocal()
	if err != nil {
		return err
	}
	if existing := local.FindByID(b.ID); existing != nil {
		*existing = b
	} else {
		local.Brokers = append(local.Brokers, b)
	}
	if err := os.MkdirAll(filepath.Dir(LocalPath()), 0o700); err != nil {
		return err
	}
	return local.Save(LocalPath())
}

func withLocal(db *BrokerDatabase, err error) (*BrokerDatabase, error) {
	if err != nil {
		return db, err
	}
	local, err := LoadLocal()
	if err != nil {
		return nil, fmt.Errorf("your own broker entries (%s): %w", LocalPath(), err)
	}
	for _, b := range local.Brokers {
		if existing := db.FindByID(b.ID); existing != nil {
			*existing = b
		} else {
			db.Brokers = append(db.Brokers, b)
		}
	}
	return db, nil
}

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

// letters that don't decompose into base letter + accent under NFD
var foldLetters = strings.NewReplacer("ß", "ss", "ø", "o", "æ", "ae", "œ", "oe", "ł", "l", "đ", "d", "þ", "th")

// NewID derives an id from a broker name, folding diacritics so EU company
// names stay readable ("Corner Shop OÜ" -> "corner-shop-ou").
func NewID(name string) string {
	var b strings.Builder
	for _, r := range norm.NFD.String(foldLetters.Replace(strings.ToLower(strings.TrimSpace(name)))) {
		if !unicode.Is(unicode.Mn, r) {
			b.WriteRune(r)
		}
	}
	return strings.Trim(slugRe.ReplaceAllString(b.String(), "-"), "-")
}
