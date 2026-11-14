package config

import (
	"strings"
	"testing"
	"time"
)

var checkDay = time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC)

func TestValidateProfileFieldsDates(t *testing.T) {
	for _, tc := range []struct{ dob, want string }{
		{"2000-11-30", ""},
		{"2000-11-31", "doesn't exist"},
		{"2001-02-29", "doesn't exist"},
		{"2000-02-29", ""},
		{"2000-13-01", "doesn't exist"},
		{"31.11.2000", "YYYY-MM-DD"},
		{"2026-11-15", "future"},
		{"2026-11-14", ""},
		{"1899-12-31", "1900"},
	} {
		got := ValidateProfileFields(Profile{DateOfBirth: tc.dob}, checkDay)["date_of_birth"]
		if (tc.want == "") != (got == "") || !strings.Contains(got, tc.want) {
			t.Errorf("%s: got %q, want %q", tc.dob, got, tc.want)
		}
	}
}

func TestValidateProfileFieldsPhoneZipText(t *testing.T) {
	ok := Profile{FirstName: "Māris", Phone: "+371 2012 3456", ZipCode: "LV-1050", Address: "Brīvības iela 1, dz. 2", City: "Rīga",
		AdditionalPhones: []string{"(555) 123-4567"}, PreviousAddresses: []string{"1 Old Rd, Leeds LS1 1AA"}, NameVariants: []string{"Maris"}}
	if errs := ValidateProfileFields(ok, checkDay); len(errs) != 0 {
		t.Fatalf("valid profile rejected: %v", errs)
	}
	for name, p := range map[string]Profile{
		"phone":              {Phone: "call me"},
		"zip_code":           {ZipCode: "!!"},
		"additional_phones":  {AdditionalPhones: []string{"12"}},
		"first_name":         {FirstName: "A\nB"},
		"city":               {City: strings.Repeat("x", 101)},
		"address":            {Address: strings.Repeat("x", 201)},
		"name_variants":      {NameVariants: make([]string, 21)},
		"previous_addresses": {PreviousAddresses: []string{"a\x00b"}},
	} {
		if ValidateProfileFields(p, checkDay)[name] == "" {
			t.Errorf("%s not rejected", name)
		}
	}
	if ValidateProfileFields(Profile{Phone: "+1234567890123456"}, checkDay)["phone"] == "" {
		t.Error("16 digits accepted")
	}
	if ValidateProfileFields(Profile{Address: strings.Repeat("x", 150)}, checkDay)["address"] != "" {
		t.Error("150-char address rejected (limit is 200)")
	}
}
