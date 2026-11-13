//go:build !windows

package schedule

import (
	"os/exec"
	"testing"
)

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

func TestReloadAfterExitReportsAHelperThatWillNotStart(t *testing.T) {
	orig := execCommand
	t.Cleanup(func() { execCommand = orig })
	execCommand = func(string, ...string) *exec.Cmd { return exec.Command("/definitely/not/a/shell") }
	if err := reloadLaunchdAfterExit("gui/501", "/tmp/x.plist"); err == nil {
		t.Error("a helper that can't start must be an error, not a schedule left half-migrated in silence")
	}
}
