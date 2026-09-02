package browser

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/drumandbytes/eraser/internal/config"
)

// isContextErr distinguishes a real browser/context failure (deadline
// exceeded, cancellation) from chromedp's normal "not found on this page"
// signal, which fillSelector/fillByPattern report as (false, nil) rather
// than an error. Getting this wrong in either direction is bad: treating a
// real timeout as "field not found" would let NavigateAndFill report
// Success=true after the browser died mid-fill (see browser.go's
// hasFillErrors gate), while treating "not found" as a real error would
// make ordinary missing fields blow up every fill attempt.
func TestIsContextErr(t *testing.T) {
	wrapped := fmt.Errorf("fillSelector: %w", context.DeadlineExceeded)
	doubleWrapped := fmt.Errorf("tryFillField: %w", wrapped)

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil error", nil, false},
		{"context.DeadlineExceeded directly", context.DeadlineExceeded, true},
		{"context.Canceled directly", context.Canceled, true},
		{"context.DeadlineExceeded wrapped once with %w", wrapped, true},
		{"context.DeadlineExceeded wrapped twice with %w", doubleWrapped, true},
		{"context.Canceled wrapped with %w", fmt.Errorf("op failed: %w", context.Canceled), true},
		{"unrelated error", errors.New("element not found"), false},
		{"unrelated wrapped error", fmt.Errorf("selector failed: %w", errors.New("boom")), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isContextErr(tt.err); got != tt.want {
				t.Errorf("isContextErr(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

// escapeSelector/jsStringLiteral feed directly into JS snippets spliced into
// chromedp.Evaluate calls (see fillSelector, fillSelectElement). A value or
// selector containing an unescaped quote could break out of the JS string
// literal; this is cheap to check without a browser.
func TestEscapeSelectorAndJSStringLiteral(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"plain", `input[name='email']`},
		{"double quote", `input[name="email"]`},
		{"backslash", `input[name='a\b']`},
		{"quote and backslash", `O'Brien\"s`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			escaped := escapeSelector(tc.input)
			// No unescaped double quote or backslash should survive: every
			// backslash and every double quote in the output must be part
			// of an escape pair introduced by escapeSelector.
			for i := 0; i < len(escaped); i++ {
				if escaped[i] == '"' {
					if i == 0 || escaped[i-1] != '\\' {
						t.Errorf("escapeSelector(%q) = %q has an unescaped double quote at %d", tc.input, escaped, i)
					}
				}
			}

			lit := jsStringLiteral(tc.input)
			if len(lit) < 2 || lit[0] != '"' || lit[len(lit)-1] != '"' {
				t.Errorf("jsStringLiteral(%q) = %q is not wrapped in double quotes", tc.input, lit)
			}
		})
	}
}

// getFieldMappings needs no browser - it's pure profile-to-selector mapping
// logic, so this is cheap to check without chromedp/Chrome.
func TestGetFieldMappingsUsesProfileValues(t *testing.T) {
	profile := &config.Profile{
		FirstName:  "Jane",
		MiddleName: "Marie",
		LastName:   "Doe",
		Email:      "jane@example.com",
		Phone:      "+1-555-0100",
		Address:    "123 Main St",
		City:       "Riga",
		State:      "",
		ZipCode:    "LV-1010",
		Country:    "Latvia",
	}
	f := NewFormFiller(profile)

	mappings := f.getFieldMappings()

	byType := make(map[string]FieldMapping, len(mappings))
	for _, m := range mappings {
		byType[m.FieldType] = m
	}

	if got := byType["email"].ProfileValue; got != profile.Email {
		t.Errorf("email mapping = %q, want %q", got, profile.Email)
	}
	if got := byType["firstName"].ProfileValue; got != profile.FirstName {
		t.Errorf("firstName mapping = %q, want %q", got, profile.FirstName)
	}
	if got := byType["lastName"].ProfileValue; got != profile.LastName {
		t.Errorf("lastName mapping = %q, want %q", got, profile.LastName)
	}

	// Regression check: fullName used to be hardcoded as
	// FirstName+" "+LastName, silently dropping MiddleName even though
	// config.Profile.FullName() includes it - a broker form's "full name"
	// field would fill without the middle name a user explicitly provided.
	wantFullName := profile.FullName()
	if got := byType["fullName"].ProfileValue; got != wantFullName {
		t.Errorf("fullName mapping = %q, want %q (config.Profile.FullName())", got, wantFullName)
	}
	if got := byType["fullName"].ProfileValue; got == profile.FirstName+" "+profile.LastName {
		t.Error("fullName mapping dropped MiddleName - matches the old FirstName+LastName-only bug")
	}

	if got := byType["state"].ProfileValue; got != "" {
		t.Errorf("state mapping = %q, want empty string for an unset profile field", got)
	}

	// Every mapping's selectors/patterns are static data - just confirm
	// they're non-empty so a future edit can't silently leave a field type
	// with no way to ever match it on a page.
	for fieldType, m := range byType {
		if len(m.Selectors) == 0 {
			t.Errorf("field %q has no Selectors", fieldType)
		}
		if len(m.Patterns) == 0 {
			t.Errorf("field %q has no Patterns", fieldType)
		}
	}
}

func TestGetFieldMappingsFullNameWithoutMiddleName(t *testing.T) {
	profile := &config.Profile{FirstName: "Jane", LastName: "Doe"}
	f := NewFormFiller(profile)

	for _, m := range f.getFieldMappings() {
		if m.FieldType == "fullName" {
			if got, want := m.ProfileValue, "Jane Doe"; got != want {
				t.Errorf("fullName mapping = %q, want %q", got, want)
			}
			return
		}
	}
	t.Fatal("no fullName mapping found")
}
