//go:build windows

package schedule

import "errors"

// There is no launchd on Windows; Migrate returns before it would call this.
func reloadLaunchdAfterExit(domain, plist string) error {
	return errors.New("launchd is not available on windows")
}
