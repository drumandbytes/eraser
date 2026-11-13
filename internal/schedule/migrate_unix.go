//go:build !windows

package schedule

import (
	"fmt"
	"os"
	"strings"
	"syscall"
)

// reloadLaunchdAfterExit starts a helper in its own session (launchd kills the
// rest of a job's process group when the job ends, but not a new session) that
// waits for this process to exit, then reloads the agent. It gives up waiting
// after two hours.
func reloadLaunchdAfterExit(domain, plist string) error {
	script := fmt.Sprintf(
		`n=0; while kill -0 %d 2>/dev/null && [ "$n" -lt 7200 ]; do sleep 1; n=$((n+1)); done; launchctl bootout %s 2>/dev/null; launchctl bootstrap %s %s`,
		os.Getpid(), shellQuote(domain+"/"+launchdLabel), shellQuote(domain), shellQuote(plist))
	cmd := execCommand("/bin/sh", "-c", script)
	cmd.Dir = "/"
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

// shellQuote single-quotes s for /bin/sh.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
