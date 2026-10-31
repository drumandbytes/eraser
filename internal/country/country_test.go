package country

import "testing"

func TestCode(t *testing.T) {
	for in, want := range map[string]string{
		"DE": "DE", "de": "DE", " Germany ": "DE", "UK": "GB", "Great Britain": "GB",
		"United States": "US", "USA": "US", "Latvia": "LV", "Mexico": "MX", "Czechia": "CZ",
	} {
		if got, ok := Code(in); !ok || got != want {
			t.Errorf("Code(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
	for _, in := range []string{"", "Atlantis", "ZZ", "EU", "XYZ"} {
		if got, ok := Code(in); ok {
			t.Errorf("Code(%q) = %q, want unresolved", in, got)
		}
	}
}

func TestNameAndDisplay(t *testing.T) {
	if got := Name("de"); got != "Germany" {
		t.Errorf("Name(de) = %q", got)
	}
	if Name("ZZ") != "" {
		t.Error("unknown code should have no name")
	}
	if got := Display("DE"); got != "Germany" {
		t.Errorf("Display(DE) = %q", got)
	}
	if got := Display("Atlantis"); got != "Atlantis" {
		t.Errorf("unresolved value should pass through, got %q", got)
	}
}

func TestAll(t *testing.T) {
	all := All()
	if len(all) < 200 {
		t.Fatalf("only %d countries", len(all))
	}
	seen := map[string]bool{}
	for _, e := range all {
		if seen[e.Name] || e.Code == "UK" {
			t.Fatalf("duplicate or reserved entry %+v", e)
		}
		seen[e.Name] = true
	}
	for _, code := range []string{"EZ", "UN", "AC", "IC", "EA"} {
		if Name(code) != "" {
			t.Errorf("%s is not a country but is selectable", code)
		}
	}
	if len(all) > 252 {
		t.Errorf("%d entries; ISO 3166-1 has 249 plus Kosovo", len(all))
	}
	if got := Name("GB"); got != "United Kingdom" {
		t.Errorf("Name(GB) = %q", got)
	}
	for i := 1; i < len(all); i++ {
		if all[i-1].Name > all[i].Name {
			t.Fatalf("not sorted at %s/%s", all[i-1].Name, all[i].Name)
		}
	}
}
