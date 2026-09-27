package schedule

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// Every is how often the OS job runs. Six hours rather than daily: the send
// cap is a rolling 24h window, so a run exactly 24h after the last one still
// sees that run's sends inside the window and sends nothing. Runs with
// nothing due are cheap no-ops.
const Every = "every 6 hours"

const (
	launchdLabel = "com.drumandbytes.eraser.auto"
	systemdUnit  = "eraser-auto"
)

// Job is what the OS scheduler runs: `<Exe> auto --once --config <ConfigPath>`.
type Job struct {
	Exe        string
	ConfigPath string
	LogPath    string // launchd only; systemd logs to the journal
}

func (j Job) args() []string {
	return []string{j.Exe, "auto", "--once", "--config", j.ConfigPath}
}

// UnderOSJob reports whether this process was started by the OS job, from
// the environment launchd and systemd set for their services.
func UnderOSJob() bool {
	return os.Getenv("XPC_SERVICE_NAME") == launchdLabel || os.Getenv("INVOCATION_ID") != ""
}

// Supported reports whether Install can set up an OS job on this platform.
func Supported() bool {
	return runtime.GOOS == "darwin" || runtime.GOOS == "linux"
}

func launchdPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "LaunchAgents", launchdLabel+".plist"), nil
}

func systemdDir() (string, error) {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "systemd", "user"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "systemd", "user"), nil
}

// unitFile is the file whose presence means the job is installed.
func unitFile() (string, error) {
	switch runtime.GOOS {
	case "darwin":
		return launchdPath()
	case "linux":
		dir, err := systemdDir()
		return filepath.Join(dir, systemdUnit+".timer"), err
	}
	return "", fmt.Errorf("no OS scheduler support on %s", runtime.GOOS)
}

// Installed reports whether the OS job is set up. False on unsupported
// platforms.
func Installed() bool {
	path, err := unitFile()
	if err != nil {
		return false
	}
	_, err = os.Stat(path)
	return err == nil
}

// Install writes and loads the OS job, replacing any earlier one.
func Install(j Job) error {
	switch runtime.GOOS {
	case "darwin":
		path, err := launchdPath()
		if err != nil {
			return err
		}
		if err := writeFile(path, renderPlist(j)); err != nil {
			return err
		}
		domain := "gui/" + strconv.Itoa(os.Getuid())
		_ = exec.Command("launchctl", "bootout", domain+"/"+launchdLabel).Run() // not loaded yet is fine
		return run("launchctl", "bootstrap", domain, path)
	case "linux":
		dir, err := systemdDir()
		if err != nil {
			return err
		}
		service, timer := renderSystemd(j)
		if err := writeFile(filepath.Join(dir, systemdUnit+".service"), service); err != nil {
			return err
		}
		if err := writeFile(filepath.Join(dir, systemdUnit+".timer"), timer); err != nil {
			return err
		}
		if err := run("systemctl", "--user", "daemon-reload"); err != nil {
			return err
		}
		return run("systemctl", "--user", "enable", "--now", systemdUnit+".timer")
	}
	return fmt.Errorf("no OS scheduler support on %s", runtime.GOOS)
}

// Remove unloads and deletes the OS job. Removing a job that isn't
// installed is not an error.
func Remove() error {
	switch runtime.GOOS {
	case "darwin":
		path, err := launchdPath()
		if err != nil {
			return err
		}
		_ = exec.Command("launchctl", "bootout", "gui/"+strconv.Itoa(os.Getuid())+"/"+launchdLabel).Run()
		return removeFile(path)
	case "linux":
		dir, err := systemdDir()
		if err != nil {
			return err
		}
		_ = exec.Command("systemctl", "--user", "disable", "--now", systemdUnit+".timer").Run()
		if err := removeFile(filepath.Join(dir, systemdUnit+".timer")); err != nil {
			return err
		}
		if err := removeFile(filepath.Join(dir, systemdUnit+".service")); err != nil {
			return err
		}
		return run("systemctl", "--user", "daemon-reload")
	}
	return fmt.Errorf("no OS scheduler support on %s", runtime.GOOS)
}

func renderPlist(j Job) string {
	esc := func(s string) string {
		var b bytes.Buffer
		_ = xml.EscapeText(&b, []byte(s))
		return b.String()
	}
	var args strings.Builder
	for _, a := range j.args() {
		fmt.Fprintf(&args, "\t\t<string>%s</string>\n", esc(a))
	}
	var times strings.Builder
	for _, h := range []int{0, 6, 12, 18} {
		fmt.Fprintf(&times, "\t\t<dict><key>Hour</key><integer>%d</integer><key>Minute</key><integer>7</integer></dict>\n", h)
	}
	// StartCalendarInterval (unlike StartInterval) runs a missed slot on
	// wake, so a laptop that slept through 06:07 still gets its run.
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>%s</string>
	<key>ProgramArguments</key>
	<array>
%s	</array>
	<key>StartCalendarInterval</key>
	<array>
%s	</array>
	<key>StandardOutPath</key>
	<string>%s</string>
	<key>StandardErrorPath</key>
	<string>%s</string>
</dict>
</plist>
`, launchdLabel, args.String(), times.String(), esc(j.LogPath), esc(j.LogPath))
}

func renderSystemd(j Job) (service, timer string) {
	quoted := make([]string, 0, len(j.args()))
	for _, a := range j.args() {
		// systemd's quoting: double quotes, with \ and " escaped; % is a
		// specifier and must be doubled.
		a = strings.NewReplacer(`\`, `\\`, `"`, `\"`, `%`, `%%`).Replace(a)
		quoted = append(quoted, `"`+a+`"`)
	}
	service = fmt.Sprintf(`[Unit]
Description=Eraser: send due removal requests and check the inbox

[Service]
Type=oneshot
ExecStart=%s
`, strings.Join(quoted, " "))
	// Persistent=true runs a slot missed while the machine was off.
	timer = `[Unit]
Description=Run Eraser every 6 hours

[Timer]
OnCalendar=*-*-* 00/6:07:00
Persistent=true

[Install]
WantedBy=timers.target
`
	return service, timer
}

func writeFile(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("failed to create %s: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write %s: %w", path, err)
	}
	return nil
}

func removeFile(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove %s: %w", path, err)
	}
	return nil
}

func run(name string, args ...string) error {
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}
