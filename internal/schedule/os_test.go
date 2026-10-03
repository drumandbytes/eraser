package schedule

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/drumandbytes/eraser/internal/config"
)

// fakeOS points the seams at a temp HOME and a recording command stub, so
// Install/Remove run for real against files only. Commands named in fail
// exit non-zero.
func fakeOS(t *testing.T, platform string, fail ...string) *[]string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	var calls []string
	origExec, origOS := execCommand, goos
	t.Cleanup(func() { execCommand, goos = origExec, origOS })
	goos = platform
	execCommand = func(name string, args ...string) *exec.Cmd {
		line := strings.Join(append([]string{name}, args...), " ")
		calls = append(calls, line)
		for _, f := range fail {
			if strings.Contains(line, f) {
				return exec.Command("false")
			}
		}
		return exec.Command("true")
	}
	return &calls
}

var testJob = Job{Exe: "/opt/eraser", ConfigPath: "/home/j/.eraser/config.yaml", LogPath: "/home/j/.eraser/auto.log"}

func TestInstallAndRemoveLaunchd(t *testing.T) {
	calls := fakeOS(t, "darwin")
	if !Supported() || Installed() {
		t.Fatal("darwin should be supported and start uninstalled")
	}
	if err := Install(testJob); err != nil {
		t.Fatalf("Install: %v", err)
	}
	path, _ := launchdPath()
	plist, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(plist), "<string>/opt/eraser</string>") {
		t.Fatalf("plist = %s, %v", plist, err)
	}
	if !Installed() {
		t.Error("Installed() false after Install")
	}
	if got := strings.Join(*calls, "\n"); !strings.Contains(got, "launchctl bootout gui/") || !strings.Contains(got, "launchctl bootstrap gui/") {
		t.Errorf("launchctl calls = %v", *calls)
	}

	if err := Remove(); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if Installed() {
		t.Error("still installed after Remove")
	}
	if err := Remove(); err != nil { // removing twice is fine
		t.Errorf("second Remove: %v", err)
	}
}

func TestInstallLaunchdBootstrapFailure(t *testing.T) {
	fakeOS(t, "darwin", "bootstrap")
	err := Install(testJob)
	if err == nil || !strings.Contains(err.Error(), "launchctl bootstrap") {
		t.Errorf("err = %v", err)
	}
}

func TestInstallAndRemoveSystemd(t *testing.T) {
	calls := fakeOS(t, "linux")
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	if err := Install(testJob); err != nil {
		t.Fatalf("Install: %v", err)
	}
	dir := filepath.Join(xdg, "systemd", "user")
	for _, f := range []string{systemdUnit + ".service", systemdUnit + ".timer"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Errorf("%s not written: %v", f, err)
		}
	}
	if !Installed() {
		t.Error("Installed() false after Install")
	}
	if got := strings.Join(*calls, "\n"); !strings.Contains(got, "systemctl --user daemon-reload") || !strings.Contains(got, "enable --now eraser-auto.timer") {
		t.Errorf("systemctl calls = %v", *calls)
	}
	if err := Remove(); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if Installed() {
		t.Error("still installed after Remove")
	}
}

func TestSystemdDirDefaultsUnderHome(t *testing.T) {
	fakeOS(t, "linux")
	dir, err := systemdDir()
	if err != nil || dir != filepath.Join(os.Getenv("HOME"), ".config", "systemd", "user") {
		t.Errorf("systemdDir = %q, %v", dir, err)
	}
}

func TestSystemdFailures(t *testing.T) {
	fakeOS(t, "linux", "daemon-reload")
	if err := Install(testJob); err == nil {
		t.Error("Install ignored a daemon-reload failure")
	}
	if err := Remove(); err == nil {
		t.Error("Remove ignored a daemon-reload failure")
	}

	fakeOS(t, "linux", "enable")
	if err := Install(testJob); err == nil {
		t.Error("Install ignored an enable failure")
	}
}

// Unit files can't be written where a file blocks the directory.
func TestInstallWriteFailures(t *testing.T) {
	for _, platform := range []string{"darwin", "linux"} {
		fakeOS(t, platform)
		home := os.Getenv("HOME")
		blocker := filepath.Join(home, "Library")
		if platform == "linux" {
			blocker = filepath.Join(home, ".config")
		}
		if err := os.WriteFile(blocker, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := Install(testJob); err == nil {
			t.Errorf("%s: Install under a blocked directory succeeded", platform)
		}
	}
}

func TestRemoveFailsOnUndeletableFile(t *testing.T) {
	fakeOS(t, "darwin")
	path, _ := launchdPath()
	// A non-empty directory where the plist should be can't be removed.
	if err := os.MkdirAll(filepath.Join(path, "x"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := Remove(); err == nil {
		t.Error("Remove ignored a failed delete")
	}

	fakeOS(t, "linux")
	dir, _ := systemdDir()
	if err := os.MkdirAll(filepath.Join(dir, systemdUnit+".timer", "x"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := Remove(); err == nil {
		t.Error("Remove ignored a failed timer delete")
	}
	if err := os.RemoveAll(filepath.Join(dir, systemdUnit+".timer")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, systemdUnit+".service", "x"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := Remove(); err == nil {
		t.Error("Remove ignored a failed service delete")
	}
}

func TestUnsupportedPlatform(t *testing.T) {
	fakeOS(t, "windows")
	if Supported() || Installed() {
		t.Error("windows reported as supported/installed")
	}
	if err := Install(testJob); err == nil {
		t.Error("Install on windows succeeded")
	}
	if err := Remove(); err == nil {
		t.Error("Remove on windows succeeded")
	}
}

func TestNoHomeDirectory(t *testing.T) {
	fakeOS(t, "darwin")
	t.Setenv("HOME", "")
	if _, err := os.UserHomeDir(); err == nil {
		t.Skip("home directory resolves without $HOME here")
	}
	if Installed() {
		t.Error("installed without a home directory")
	}
	if err := Install(testJob); err == nil {
		t.Error("darwin Install without a home succeeded")
	}
	if err := Remove(); err == nil {
		t.Error("darwin Remove without a home succeeded")
	}
	goos = "linux"
	if err := Install(testJob); err == nil {
		t.Error("linux Install without a home succeeded")
	}
	if err := Remove(); err == nil {
		t.Error("linux Remove without a home succeeded")
	}
}

func TestUnderOSJob(t *testing.T) {
	t.Setenv("XPC_SERVICE_NAME", "")
	t.Setenv("INVOCATION_ID", "")
	if UnderOSJob() {
		t.Error("UnderOSJob with no service env")
	}
	t.Setenv("XPC_SERVICE_NAME", launchdLabel)
	if !UnderOSJob() {
		t.Error("launchd env not detected")
	}
	t.Setenv("XPC_SERVICE_NAME", "")
	t.Setenv("INVOCATION_ID", "abc")
	if !UnderOSJob() {
		t.Error("systemd env not detected")
	}
}

func stubExecutable(t *testing.T, path string, err error) {
	t.Helper()
	orig := executable
	t.Cleanup(func() { executable = orig })
	executable = func() (string, error) { return path, err }
}

func TestStableExecutable(t *testing.T) {
	stubExecutable(t, "", errors.New("no /proc"))
	if _, err := StableExecutable(); err == nil {
		t.Error("executable error swallowed")
	}

	stubExecutable(t, filepath.Join(os.TempDir(), "go-build123", "b001", "exe", "eraser"), nil)
	if _, err := StableExecutable(); err == nil || !strings.Contains(err.Error(), "go run") {
		t.Errorf("temp build: %v", err)
	}

	// A real install: a binary outside TMPDIR, also reachable via PATH
	// through a symlink, resolves to the PATH entry.
	binDir, dir := outsideTMPDIR(t), t.TempDir()
	bin := filepath.Join(binDir, "eraser-real")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "eraser")
	if err := os.Symlink(bin, link); err != nil {
		t.Fatal(err)
	}
	real, _ := filepath.EvalSymlinks(bin)
	stubExecutable(t, bin, nil)

	t.Setenv("PATH", dir)
	if got, err := StableExecutable(); err != nil || got != link {
		t.Errorf("with eraser on PATH = %q, %v; want the PATH symlink %q", got, err, link)
	}
	t.Setenv("PATH", "")
	if got, err := StableExecutable(); err != nil || got != real {
		t.Errorf("without PATH = %q, %v; want %q", got, err, real)
	}
}

// outsideTMPDIR returns a directory outside os.TempDir by moving TMPDIR into
// a subdirectory of the test's temp dir.
func outsideTMPDIR(t *testing.T) string {
	t.Helper()
	base := t.TempDir()
	sub := filepath.Join(base, "tmp")
	if err := os.Mkdir(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", sub)
	bin := filepath.Join(base, "bin")
	if err := os.Mkdir(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	return bin
}

func TestNewJob(t *testing.T) {
	valid := &config.Config{
		Profiles: []config.NamedProfile{{ID: "jane", Profile: config.Profile{FirstName: "Jane", LastName: "Doe", Email: "jane@example.com"}}},
		Email:    config.EmailConfig{From: "jane@example.com", SMTP: config.SMTPConfig{Host: "smtp.example.com", Port: 465, Username: "jane"}},
	}
	if _, err := NewJob(&config.Config{}, "c.yaml"); err == nil || !strings.Contains(err.Error(), "fix your config") {
		t.Errorf("invalid config: %v", err)
	}
	manual := *valid
	manual.Options.SendMode = "manual"
	if _, err := NewJob(&manual, "c.yaml"); err == nil || !strings.Contains(err.Error(), "nothing to automate") {
		t.Errorf("manual without inbox: %v", err)
	}

	stubExecutable(t, "", errors.New("gone"))
	if _, err := NewJob(valid, "c.yaml"); err == nil {
		t.Error("executable error swallowed")
	}

	bin := filepath.Join(outsideTMPDIR(t), "eraser")
	if err := os.WriteFile(bin, nil, 0o755); err != nil {
		t.Fatal(err)
	}
	stubExecutable(t, bin, nil)
	t.Setenv("PATH", "")
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	j, err := NewJob(valid, cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if j.ConfigPath != cfgPath || j.LogPath != filepath.Join(filepath.Dir(cfgPath), "auto.log") {
		t.Errorf("job = %+v", j)
	}
}

func TestStateErrors(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope")
	if err := SaveState(missing, State{}); err == nil {
		t.Error("SaveState into a missing dir succeeded")
	}
	dir := t.TempDir()
	if err := os.Mkdir(statePath(dir), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadState(dir); err == nil {
		t.Error("LoadState of a directory succeeded")
	}
	if _, _, err := TryLock(missing); err == nil {
		t.Error("TryLock in a missing dir succeeded")
	}
}
