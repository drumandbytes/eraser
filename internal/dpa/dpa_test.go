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

func TestDescribe(t *testing.T) {
	lv := ForCountry("Latvia")
	if got := lv.Describe(); got != "Datu valsts inspekcija (Data State Inspectorate) (DVI) - https://www.dvi.gov.lv" {
		t.Errorf("Describe() = %q", got)
	}
}

func TestDescribeWithoutAcronym(t *testing.T) {
	a := Authority{Authority: "Data State Inspectorate", Website: "https://www.dvi.gov.lv"}
	if got := a.Describe(); got != "Data State Inspectorate - https://www.dvi.gov.lv" {
		t.Errorf("Describe = %q", got)
	}
	if ForCountry("  ") != nil || ForCountry("Atlantis") != nil {
		t.Error("ForCountry matched nonsense")
	}
	if a := ForCountry("UK"); a == nil || a.Code != "GB" {
		t.Errorf("UK alias = %+v", a)
	}
}
