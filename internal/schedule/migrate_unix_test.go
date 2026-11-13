//go:build !windows

package schedule

import "testing"

func TestShellQuote(t *testing.T) {
	for in, want := range map[string]string{
		`plain`:          `'plain'`,
		`it's`:           `'it'\''s'`,
		`/a b/$HOME/;ls`: `'/a b/$HOME/;ls'`,
	} {
		if got := shellQuote(in); got != want {
			t.Errorf("shellQuote(%q) = %s, want %s", in, got, want)
		}
	}
}
