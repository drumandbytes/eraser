package dpa

import "testing"

func TestAll(t *testing.T) {
	all := All()
	if len(all) < 40 {
		t.Fatalf("expected 40+ authorities, got %d", len(all))
	}
	for _, a := range all {
		if a.Country == "" || a.Authority == "" || a.Website == "" || a.Code == "" || a.Region == "" {
			t.Errorf("incomplete entry: %+v", a)
		}
	}
}

func TestForCountry(t *testing.T) {
	cases := map[string]string{
		"Latvia":                   "LV",
		"latvia":                   "LV",
		"LV":                       "LV",
		"Germany":                  "DE",
		"UK":                       "GB",
		"United Kingdom":           "GB",
		"Czechia":                  "CZ",
		"The Netherlands":          "NL",
		"USA":                      "US",
		"United States":            "US",
		"United States of America": "US",
		"California":               "US-CA",
		"Canada":                   "CA",
		"Australia":                "AU",
	}
	for in, wantCode := range cases {
		got := ForCountry(in)
		if got == nil {
			t.Errorf("ForCountry(%q) = nil", in)
			continue
		}
		if got.Code != wantCode {
			t.Errorf("ForCountry(%q).Code = %q, want %q", in, got.Code, wantCode)
		}
	}
	if ForCountry("Narnia") != nil {
		t.Error("ForCountry(unknown) should be nil")
	}
	if ForCountry("") != nil {
		t.Error("ForCountry(empty) should be nil")
	}
}

func TestByRegion(t *testing.T) {
	order, groups := ByRegion()
	if len(order) < 3 {
		t.Fatalf("expected several regions, got %v", order)
	}
	if order[0] != "European Union / EEA" {
		t.Errorf("first region = %q, want European Union / EEA", order[0])
	}
	total := 0
	for _, r := range order {
		total += len(groups[r])
	}
	if total != len(All()) {
		t.Errorf("grouped %d != All() %d", total, len(All()))
	}
}

func TestDescribe(t *testing.T) {
	lv := ForCountry("Latvia")
	if got := lv.Describe(); got != "Datu valsts inspekcija (Data State Inspectorate) (DVI) - https://www.dvi.gov.lv" {
		t.Errorf("Describe() = %q", got)
	}
}
