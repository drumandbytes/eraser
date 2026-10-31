// Package country maps between ISO 3166-1 alpha-2 codes and English country
// names. Profiles store the code; names are for display and form fields.
package country

import (
	"slices"
	"strings"
	"sync"

	"golang.org/x/text/language"
	"golang.org/x/text/language/display"
)

// Entry is one selectable country.
type Entry struct {
	Code string
	Name string
}

// aliases covers common profile values the official English names miss.
var aliases = map[string]string{
	"uk": "GB", "u.k.": "GB", "great britain": "GB", "britain": "GB",
	"england": "GB", "scotland": "GB", "wales": "GB", "northern ireland": "GB",
	"czech republic": "CZ", "the netherlands": "NL", "holland": "NL",
	"usa": "US", "u.s.": "US", "u.s.a.": "US", "united states of america": "US", "america": "US",
	"deutschland": "DE", "österreich": "AT", "españa": "ES", "italia": "IT",
}

var (
	once    sync.Once
	entries []Entry
	byName  map[string]string
)

func load() {
	once.Do(func() {
		byName = map[string]string{}
		for a := 'A'; a <= 'Z'; a++ {
			for b := 'A'; b <= 'Z'; b++ {
				code := string([]rune{a, b})
				r, err := language.ParseRegion(code)
				// Canonicalize drops deprecated and alias codes (DD -> DE, UK -> GB).
				if err != nil || !r.IsCountry() || r.String() != code || r.Canonicalize() != r {
					continue
				}
				name := display.English.Regions().Name(r)
				if name == "" {
					continue
				}
				entries = append(entries, Entry{Code: code, Name: name})
				byName[strings.ToLower(name)] = code
			}
		}
		slices.SortFunc(entries, func(x, y Entry) int { return strings.Compare(x.Name, y.Name) })
	})
}

// All lists every country, sorted by name.
func All() []Entry {
	load()
	return entries
}

// Code resolves an ISO alpha-2 code or an English country name (or common
// alias) to the upper-case code. ok is false when it can't be resolved.
func Code(s string) (code string, ok bool) {
	load()
	q := strings.ToLower(strings.TrimSpace(s))
	if q == "" {
		return "", false
	}
	if c, found := aliases[q]; found {
		return c, true
	}
	if len(q) == 2 {
		c := strings.ToUpper(q)
		for _, e := range entries {
			if e.Code == c {
				return c, true
			}
		}
		return "", false
	}
	c, found := byName[q]
	return c, found
}

// Name is the English name for a code, or "" when the code is unknown.
func Name(code string) string {
	load()
	c := strings.ToUpper(strings.TrimSpace(code))
	for _, e := range entries {
		if e.Code == c {
			return e.Name
		}
	}
	return ""
}

// Display is what to show or type into a form for a stored profile country: the
// English name when it resolves, otherwise the raw value (a legacy free-text
// entry that never resolved).
func Display(s string) string {
	if c, ok := Code(s); ok {
		return Name(c)
	}
	return strings.TrimSpace(s)
}
