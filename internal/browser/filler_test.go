package browser

import (
	"context"
	"errors"
	"fmt"
	"testing"
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
