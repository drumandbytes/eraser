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
	"time"

	"github.com/drumandbytes/eraser/internal/config"
)

// Interval is how often cycles run, in every mode. Six hours rather than
// daily: the send cap is a rolling 24h window, so a run exactly 24h after
// the last one still sees that run's sends inside the window and sends
// nothing. Runs with nothing due are cheap no-ops.
const Interval = 6 * time.Hour

// Every describes Interval for messages.
const Every = "every 6 hours"

// osSlots are the local hours the OS job fires at, on minute 7.
var osSlots = []int{0, 6, 12, 18}

// NextOSRun is when the installed OS job next fires after now.
func NextOSRun(now time.Time) time.Time {
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 7, 0, 0, now.Location())
	for d := 0; d < 2; d++ {
		for _, h := range osSlots {
			if t := day.AddDate(0, 0, d).Add(time.Duration(h) * time.Hour); t.After(now) {
				return t
			}
		}
	}
	return day.AddDate(0, 0, 1)
}

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

// NewJob checks that cfg can run unattended and builds the OS job for it.
// It refuses configs that would fail on every run.
func NewJob(cfg *config.Config, configPath string) (Job, error) {
	if err := cfg.Validate(); err != nil {
		return Job{}, fmt.Errorf("fix your config before scheduling: %w", err)
	}
	if cfg.IsManualSend() && len(cfg.ConfiguredInboxes()) == 0 {
		return Job{}, fmt.Errorf("nothing to automate: send_mode is manual and no inbox is configured")
	}
	abs, err := filepath.Abs(configPath)
	if err != nil {
		return Job{}, err
	}
	exe, err := StableExecutable()
	if err != nil {
		return Job{}, err
	}
	return Job{Exe: exe, ConfigPath: abs, LogPath: filepath.Join(filepath.Dir(abs), "auto.log")}, nil
}

// StableExecutable is the path the OS job should run. It prefers the eraser
// on PATH (e.g. Homebrew's symlink, which survives upgrades) when that's the
// same binary as this one, and refuses a 'go run' temp build.
func StableExecutable() (string, error) {
	self, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("failed to find the eraser binary: %w", err)
	}
	selfReal, err := filepath.EvalSymlinks(self)
	if err != nil {
		selfReal = self
	}
	tmp, err := filepath.EvalSymlinks(os.TempDir())
	if err != nil {
		tmp = os.TempDir()
	}
	if strings.HasPrefix(selfReal, filepath.Clean(tmp)+string(filepath.Separator)) || strings.Contains(selfReal, "go-build") {
		return "", fmt.Errorf("this is a temporary 'go run' build (%s) - build or install eraser first, then set up the schedule from that binary", selfReal)
	}
	if onPath, err := exec.LookPath("eraser"); err == nil {
		if abs, err := filepath.Abs(onPath); err == nil {
			if real, err := filepath.EvalSymlinks(abs); err == nil && real == selfReal {
				return abs, nil
			}
		}
	}
	return selfReal, nil
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
	for _, h := range osSlots {
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
