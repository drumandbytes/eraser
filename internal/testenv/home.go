// Package testenv has helpers for tests that must not touch the real home directory.
package testenv

import "testing"

// SetHome points os.UserHomeDir at dir on every OS. Unix reads HOME, Windows
// reads USERPROFILE; setting only HOME let Windows tests use the real profile.
func SetHome(t testing.TB, dir string) {
	t.Helper()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
}
