package schedule

import (
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
)

// An OS schedule installed before the rename runs the deprecated "eraser"
// alias. Migrate points it at the delistd binary instead, so removing the alias
// in the next major release can't silently stop anyone's scheduled runs.

// The executable in a plist renderPlist wrote: its first ProgramArguments entry.
var plistExeRe = regexp.MustCompile(`(?s)<key>ProgramArguments</key>\s*<array>\s*<string>([^<]*)</string>`)

// The executable in the ExecStart line renderSystemd wrote (quoted, with \" and \\ escapes).
var systemdExeRe = regexp.MustCompile(`(?m)^ExecStart="((?:[^"\\]|\\.)*)"`)

// Migrate rewrites an installed OS schedule that still runs the "eraser" alias.
// It does nothing when no schedule is installed, when it already runs delistd, or
// when there is no delistd binary to point it at. On success it tells w (if
// non-nil) in one line. It is safe to run on every start and from inside the
// scheduled run itself.
func Migrate(w io.Writer) error {
	unit, err := unitFile() // the plist, or the systemd timer
	if err != nil {
		return nil // no OS scheduler here, or no home directory: nothing can be installed
	}
	if _, err := os.Stat(unit); err != nil {
		return nil // not installed
	}
	if goos == "darwin" {
		return migrateLaunchd(w, unit)
	}
	return migrateSystemd(w, filepath.Join(filepath.Dir(unit), systemdUnit+".service")) // unitFile only succeeds on darwin and linux
}

func migrateLaunchd(w io.Writer, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	content := string(data)
	loc := plistExeRe.FindStringSubmatchIndex(content)
	if loc == nil {
		return nil
	}
	oldExe := xmlUnescape(content[loc[2]:loc[3]])
	newExe := migrationTarget(oldExe)
	if newExe == "" {
		return nil
	}
	if err := writeFileAtomic(path, content[:loc[2]]+xmlEscape(newExe)+content[loc[3]:]); err != nil {
		return err
	}
	if err := reloadLaunchd(path); err != nil {
		return fmt.Errorf("rewrote the schedule (%s -> %s) but could not reload launchd: %w", oldExe, newExe, err)
	}
	announceMigration(w, oldExe, newExe)
	return nil
}

func migrateSystemd(w io.Writer, path string) error {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	content := string(data)
	loc := systemdExeRe.FindStringSubmatchIndex(content)
	if loc == nil {
		return nil
	}
	oldExe := systemdUnescape(content[loc[2]:loc[3]])
	newExe := migrationTarget(oldExe)
	if newExe == "" {
		return nil
	}
	if err := writeFileAtomic(path, content[:loc[2]]+systemdEscape(newExe)+content[loc[3]:]); err != nil {
		return err
	}
	// Safe from inside the scheduled run too: a reload doesn't stop a service
	// that is already running, and the next slot starts the new ExecStart.
	if err := run("systemctl", "--user", "daemon-reload"); err != nil {
		return fmt.Errorf("rewrote the schedule (%s -> %s) but daemon-reload failed: %w", oldExe, newExe, err)
	}
	announceMigration(w, oldExe, newExe)
	return nil
}

// reloadLaunchd makes launchd run the rewritten plist. launchd only reads a
// plist when the agent is loaded, and unloading an agent kills its running job,
// so from inside the scheduled run (which is that job) the reload has to wait
// until this process has exited.
func reloadLaunchd(path string) error {
	domain := "gui/" + strconv.Itoa(os.Getuid())
	if UnderOSJob() {
		return reloadLaunchdAfterExit(domain, path)
	}
	_ = execCommand("launchctl", "bootout", domain+"/"+launchdLabel).Run() // not loaded is fine
	return run("launchctl", "bootstrap", domain, path)
}

// migrationTarget is the delistd binary an old schedule entry should run, or ""
// when it isn't an eraser entry or there is nothing sensible to point it at.
func migrationTarget(oldExe string) string {
	if !isBinaryNamed(oldExe, "eraser") {
		return ""
	}
	// The cask and the release archives install delistd next to eraser. This also
	// works from inside the scheduled run, where launchd's minimal PATH finds neither.
	if p := filepath.Join(filepath.Dir(oldExe), "delistd"); isExecutableFile(p) {
		return p
	}
	// Otherwise this binary, when it is delistd and not a 'go run' build.
	if exe, err := StableExecutable(); err == nil && isBinaryNamed(exe, "delistd") {
		return exe
	}
	return ""
}

func announceMigration(w io.Writer, oldExe, newExe string) {
	if w != nil {
		_, _ = fmt.Fprintf(w, "✓ Your OS schedule now runs %s instead of the deprecated %s.\n", newExe, oldExe)
	}
}

func isBinaryNamed(path, name string) bool {
	base := strings.ToLower(filepath.Base(path))
	return base == name || base == name+".exe"
}

func isExecutableFile(p string) bool {
	fi, err := os.Stat(p) // follows symlinks: Homebrew's bin entries are links
	if err != nil || !fi.Mode().IsRegular() {
		return false
	}
	return runtime.GOOS == "windows" || fi.Mode().Perm()&0o111 != 0
}

// writeFileAtomic replaces path in one step, so an interrupted run can't leave a
// half-written schedule behind.
func writeFileAtomic(path, content string) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".migrate-*")
	if err != nil {
		return fmt.Errorf("failed to write %s: %w", path, err)
	}
	name := tmp.Name()
	_, werr := tmp.WriteString(content)
	cerr := tmp.Close()
	if werr == nil {
		werr = cerr
	}
	if werr == nil {
		werr = os.Chmod(name, 0o644)
	}
	if werr == nil {
		werr = os.Rename(name, path)
	}
	if werr != nil {
		_ = os.Remove(name)
		return fmt.Errorf("failed to write %s: %w", path, werr)
	}
	return nil
}

// xmlUnescape reverses xmlEscape.
func xmlUnescape(s string) string {
	var v struct {
		S string `xml:",chardata"`
	}
	if err := xml.Unmarshal([]byte("<s>"+s+"</s>"), &v); err != nil {
		return s
	}
	return v.S
}

// systemdUnescape reverses systemdEscape.
func systemdUnescape(s string) string {
	return strings.NewReplacer(`%%`, `%`, `\"`, `"`, `\\`, `\`).Replace(s)
}
