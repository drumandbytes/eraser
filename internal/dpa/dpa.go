// Package dpa exposes the EU/EEA (+ UK) data protection supervisory-authority
// reference embedded from data/eu-dpas.yaml.
package dpa

import (
	"fmt"
	"strings"

	"github.com/eraser-privacy/eraser/data"
	"gopkg.in/yaml.v3"
)

// Authority is one supervisory authority. Website is the authority's own site
// in its own language - the resident complaining to their national authority
// reads it, so there is no separate English URL.
type Authority struct {
	Country   string `yaml:"country" json:"country"`
	Code      string `yaml:"code" json:"code"`
	EEA       bool   `yaml:"eea" json:"eea"`
	Authority string `yaml:"authority" json:"authority"`
	Acronym   string `yaml:"acronym,omitempty" json:"acronym,omitempty"`
	Website   string `yaml:"website" json:"website"`
	Notes     string `yaml:"notes,omitempty" json:"notes,omitempty"`
}

type file struct {
	Authorities []Authority `yaml:"authorities"`
}

// All returns every authority, in file order.
func All() []Authority {
	var f file
	if err := yaml.Unmarshal(data.EUDPAsYAML, &f); err != nil {
		return nil
	}
	return f.Authorities
}

// ForCountry looks an authority up by the country name or ISO code a user
// might have in their profile (case-insensitive; tolerant of common variants
// like "UK", "Czechia", "The Netherlands"). Returns nil if there's no match.
func ForCountry(name string) *Authority {
	q := strings.ToLower(strings.TrimSpace(name))
	if q == "" {
		return nil
	}
	switch q {
	case "uk", "u.k.", "great britain", "britain", "england", "scotland", "wales", "northern ireland":
		q = "united kingdom"
	case "czechia":
		q = "czech republic"
	case "the netherlands", "holland":
		q = "netherlands"
	}
	for i := range All() {
		a := All()[i]
		if strings.ToLower(a.Country) == q || strings.ToLower(a.Code) == q {
			return &a
		}
	}
	return nil
}

// Describe is a one-line "Authority (ACRONYM) - website" for CLI/report output.
func (a Authority) Describe() string {
	if a.Acronym != "" {
		return fmt.Sprintf("%s (%s) - %s", a.Authority, a.Acronym, a.Website)
	}
	return fmt.Sprintf("%s - %s", a.Authority, a.Website)
}
