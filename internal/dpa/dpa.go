// Package dpa exposes the worldwide privacy / data-protection authority
// reference embedded from data/authorities.yaml.
package dpa

import (
	"fmt"
	"strings"

	"github.com/drumandbytes/eraser/data"
	"github.com/drumandbytes/eraser/internal/country"
	"gopkg.in/yaml.v3"
)

// Authority is one privacy / data-protection authority. Website is the
// authority's own site in its own language.
type Authority struct {
	Country   string `yaml:"country" json:"country"`
	Code      string `yaml:"code" json:"code"`
	Region    string `yaml:"region" json:"region"`
	Law       string `yaml:"law,omitempty" json:"law,omitempty"` // omitted for EU/EEA (always GDPR)
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
	if err := yaml.Unmarshal(data.AuthoritiesYAML, &f); err != nil {
		return nil
	}
	return f.Authorities
}

// ForCountry looks an authority up by the ISO code or country name a profile
// holds (see package country for the accepted spellings), or by an entry's own
// code or name, e.g. "US-CA". Returns nil if there's no match.
func ForCountry(name string) *Authority {
	q := strings.TrimSpace(name)
	if q == "" {
		return nil
	}
	if code, ok := country.Code(q); ok {
		q = code
	}
	all := All()
	for i := range all {
		if strings.EqualFold(all[i].Code, q) || strings.EqualFold(all[i].Country, q) {
			return &all[i]
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
