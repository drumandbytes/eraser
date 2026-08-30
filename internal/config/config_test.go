package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTestConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}
	return path
}

const minimalConfig = `
profile:
  first_name: Test
  last_name: User
  email: test@example.com
email:
  provider: smtp
  from: test@example.com
  smtp:
    host: smtp.example.com
    port: 465
`

func TestLoadPreservesExplicitBrowserHeadlessFalse(t *testing.T) {
	path := writeTestConfig(t, minimalConfig+"pipeline:\n  browser_headless: false\n")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Pipeline.Headless() != false {
		t.Errorf("expected Headless() to stay false when explicitly set, got true")
	}

	// Round-trip through Save to make sure re-saving doesn't lose it either -
	// this is what `eraser init` now does on every update-mode run.
	savedPath := filepath.Join(t.TempDir(), "resaved.yaml")
	if err := Save(savedPath, cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}
	reloaded, err := Load(savedPath)
	if err != nil {
		t.Fatalf("reload after save: %v", err)
	}
	if reloaded.Pipeline.Headless() != false {
		t.Errorf("expected Headless() to survive a save/reload round-trip, got true")
	}
}

func TestLoadDefaultsBrowserHeadlessTrueWhenUnset(t *testing.T) {
	path := writeTestConfig(t, minimalConfig)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Pipeline.Headless() != true {
		t.Errorf("expected Headless() to default to true when unset, got false")
	}
	if cfg.Pipeline.BrowserHeadless != nil {
		t.Errorf("expected BrowserHeadless to stay nil when unset, got %v", *cfg.Pipeline.BrowserHeadless)
	}
}

func TestLoadPreservesExplicitBrowserHeadlessTrue(t *testing.T) {
	path := writeTestConfig(t, minimalConfig+"pipeline:\n  browser_headless: true\n")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Pipeline.Headless() != true {
		t.Errorf("expected Headless() to stay true when explicitly set, got false")
	}
	if cfg.Pipeline.BrowserHeadless == nil || !*cfg.Pipeline.BrowserHeadless {
		t.Errorf("expected BrowserHeadless to be a non-nil true, got %v", cfg.Pipeline.BrowserHeadless)
	}
}

func TestSlugifyProfileID(t *testing.T) {
	tests := []struct {
		name     string
		first    string
		last     string
		existing []NamedProfile
		want     string
	}{
		{"basic", "Jane", "Doe", nil, "jane-doe"},
		{"diacritics and case", "Māris", "Popēns", nil, "m-ris-pop-ns"},
		{"collision appends -2", "Jane", "Doe", []NamedProfile{{ID: "jane-doe"}}, "jane-doe-2"},
		{"collision is case-insensitive", "Jane", "Doe", []NamedProfile{{ID: "JANE-DOE"}}, "jane-doe-2"},
		{"multiple collisions increment", "Jane", "Doe", []NamedProfile{{ID: "jane-doe"}, {ID: "jane-doe-2"}}, "jane-doe-3"},
		{"empty name falls back", "", "", nil, "profile"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SlugifyProfileID(tt.first, tt.last, tt.existing)
			if got != tt.want {
				t.Errorf("SlugifyProfileID(%q, %q, %v) = %q, want %q", tt.first, tt.last, tt.existing, got, tt.want)
			}
		})
	}
}

func TestSlugifyID(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"plain", "spouse", "spouse"},
		{"spaces and punctuation", "María López!", "mar-a-l-pez"},
		{"already valid", "kid1", "kid1"},
		{"empty falls back", "", "profile"},
		{"only symbols falls back", "!!!", "profile"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SlugifyID(tt.in)
			if got != tt.want {
				t.Errorf("SlugifyID(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestSplitAndTrimAny(t *testing.T) {
	tests := []struct {
		name string
		in   string
		seps string
		want []string
	}{
		{"blank", "", ",\n", nil},
		{"whitespace only", "  \n \t ", ",\n", nil},
		{"single value", "a@example.com", ",\n", []string{"a@example.com"}},
		{"newline separated", "a@x.com\nb@x.com", ",\n", []string{"a@x.com", "b@x.com"}},
		{"comma separated", "a@x.com, b@x.com", ",\n", []string{"a@x.com", "b@x.com"}},
		// The web textarea accepts either separator, so a user who pastes the
		// CLI's comma-separated answer into it gets the same result.
		{"mixed separators", "a@x.com, b@x.com\nc@x.com", ",\n", []string{"a@x.com", "b@x.com", "c@x.com"}},
		{"blank lines and padding", " a@x.com \n\n\n  b@x.com  \n", ",\n", []string{"a@x.com", "b@x.com"}},
		{"CRLF input", "a@x.com\r\nb@x.com", ",\n\r", []string{"a@x.com", "b@x.com"}},
		// seps is a character set, not a separator string: each of these is
		// one delimiter, and callers passing a single character (the CLI)
		// behave exactly as they did with strings.Split.
		{"semicolon only, commas preserved", "1 Main St, Riga; 2 Oak Ave, Vilnius", ";",
			[]string{"1 Main St, Riga", "2 Oak Ave, Vilnius"}},
		{"comma only", "Maris, Māris", ",", []string{"Maris", "Māris"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SplitAndTrimAny(tt.in, tt.seps)
			if len(got) != len(tt.want) {
				t.Fatalf("got %d parts %+v, want %d %+v", len(got), got, len(tt.want), tt.want)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Errorf("part %d: got %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestNormalizeAdditionalEmails(t *testing.T) {
	tests := []struct {
		name    string
		primary string
		in      []string
		want    []string
	}{
		{"empty", "me@x.com", nil, nil},
		{"passthrough", "me@x.com", []string{"a@x.com", "b@x.com"}, []string{"a@x.com", "b@x.com"}},
		{"drops primary", "me@x.com", []string{"me@x.com", "a@x.com"}, []string{"a@x.com"}},
		{"drops primary case-insensitively", "Me@X.com", []string{"me@x.com", "a@x.com"}, []string{"a@x.com"}},
		{"dedupes case-insensitively", "me@x.com", []string{"A@x.com", "a@x.com"}, []string{"A@x.com"}},
		{"keeps first casing", "me@x.com", []string{"Alias@X.com", "alias@x.com"}, []string{"Alias@X.com"}},
		{"all redundant returns nil", "me@x.com", []string{"me@x.com", "ME@X.COM"}, nil},
		{"tolerates blank primary", "", []string{"a@x.com"}, []string{"a@x.com"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeAdditionalEmails(tt.primary, tt.in)
			if len(got) != len(tt.want) {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Errorf("entry %d: got %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

// The whole point of AdditionalEmails is that it reaches the rendered request,
// so pin the round-trip: a profile saved with additional emails survives a
// YAML save/load cycle with them intact.
func TestAdditionalEmailsSurviveSaveLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	cfg := &Config{
		Profile: Profile{
			FirstName:        "Test",
			LastName:         "User",
			Email:            "me@example.com",
			AdditionalEmails: []string{"old@example.com", "work@company.com"},
		},
	}
	if err := Save(path, cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	got := loaded.Profile.AdditionalEmails
	if len(got) != 2 || got[0] != "old@example.com" || got[1] != "work@company.com" {
		t.Errorf("additional emails did not survive save/load: %+v", got)
	}
}
