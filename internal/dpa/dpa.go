// Package dpa exposes the worldwide privacy / data-protection authority
// reference embedded from data/authorities.yaml.
package dpa

import (
	"fmt"
	"strings"

	"github.com/drumandbytes/eraser/data"
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

// countryAliases maps common profile `country` values to the entry's canonical
// name in authorities.yaml.
var countryAliases = map[string]string{
	"uk":                       "united kingdom",
	"u.k.":                     "united kingdom",
	"great britain":            "united kingdom",
	"britain":                  "united kingdom",
	"england":                  "united kingdom",
	"scotland":                 "united kingdom",
	"wales":                    "united kingdom",
	"northern ireland":         "united kingdom",
	"czechia":                  "czech republic",
	"the netherlands":          "netherlands",
	"holland":                  "netherlands",
	"usa":                      "united states (federal)",
	"us":                       "united states (federal)",
	"u.s.":                     "united states (federal)",
	"u.s.a.":                   "united states (federal)",
	"united states":            "united states (federal)",
	"united states of america": "united states (federal)",
	"america":                  "united states (federal)",
	"california":               "united states (california)",
}

// ForCountry looks an authority up by the country name or ISO code a user
// might have in their profile (case-insensitive, tolerant of common variants).
// Returns nil if there's no match.
func ForCountry(name string) *Authority {
	q := strings.ToLower(strings.TrimSpace(name))
	if q == "" {
		return nil
	}
	if alias, ok := countryAliases[q]; ok {
		q = alias
	}
	all := All()
	for i := range all {
		if strings.ToLower(all[i].Country) == q || strings.ToLower(all[i].Code) == q {
			return &all[i]
		}
	}
	return nil
}

// ByRegion groups the authorities by their Region, preserving first-seen order
// of both regions and entries.
func ByRegion() ([]string, map[string][]Authority) {
	var order []string
	groups := map[string][]Authority{}
	for _, a := range All() {
		if _, seen := groups[a.Region]; !seen {
			order = append(order, a.Region)
		}
		groups[a.Region] = append(groups[a.Region], a)
	}
	return order, groups
}

// Describe is a one-line "Authority (ACRONYM) - website" for CLI/report output.
func (a Authority) Describe() string {
	if a.Acronym != "" {
		return fmt.Sprintf("%s (%s) - %s", a.Authority, a.Acronym, a.Website)
	}
	return fmt.Sprintf("%s - %s", a.Authority, a.Website)
}
