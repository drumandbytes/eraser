package dpa

import "testing"

func TestAll(t *testing.T) {
	all := All()
	if len(all) < 30 {
		t.Fatalf("expected 30+ authorities, got %d", len(all))
	}
	for _, a := range all {
		if a.Country == "" || a.Authority == "" || a.Website == "" || a.Code == "" {
			t.Errorf("incomplete entry: %+v", a)
		}
	}
}

func TestForCountry(t *testing.T) {
	cases := map[string]string{
		"Latvia":          "LV",
		"latvia":          "LV",
		"LV":              "LV",
		"Germany":         "DE",
		"UK":              "GB",
		"United Kingdom":  "GB",
		"Czechia":         "CZ",
		"The Netherlands": "NL",
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

func TestLinkAndDescribe(t *testing.T) {
	lv := ForCountry("Latvia")
	if lv.Link() != "https://www.dvi.gov.lv/en" {
		t.Errorf("Link() = %q", lv.Link())
	}
	if got := lv.Describe(); got == "" || got[:4] != "Datu" {
		t.Errorf("Describe() = %q", got)
	}
}
