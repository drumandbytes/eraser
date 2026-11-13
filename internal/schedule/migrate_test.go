package schedule

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// installFakeSchedule writes the files Install would write for a job running exe.
func installFakeSchedule(t *testing.T, platform, exe string) {
	t.Helper()
	job := Job{Exe: exe, ConfigPath: "/home/user/.eraser/config.yaml", LogPath: "/home/user/.eraser/auto.log"}
	switch platform {
	case "darwin":
		path, err := launchdPath()
		if err != nil {
			t.Fatal(err)
		}
		if err := writeFile(path, renderPlist(job)); err != nil {
			t.Fatal(err)
		}
	case "linux":
		dir, err := systemdDir()
		if err != nil {
			t.Fatal(err)
		}
		service, timer := renderSystemd(job)
		if err := writeFile(filepath.Join(dir, systemdUnit+".service"), service); err != nil {
			t.Fatal(err)
		}
		if err := writeFile(filepath.Join(dir, systemdUnit+".timer"), timer); err != nil {
			t.Fatal(err)
		}
	}
}

// binaries creates executable stand-ins named names in dir (made if needed).
func binaries(t *testing.T, dir string, names ...string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

// runningAs makes this process look like it was started as exe.
func runningAs(t *testing.T, exe string) {
	t.Helper()
	orig := executable
	t.Cleanup(func() { executable = orig })
	executable = func() (string, error) { return exe, nil }
}

// notInOSJob clears the variables launchd and systemd set, which a CI runner
// may or may not have.
func notInOSJob(t *testing.T) {
	t.Helper()
	t.Setenv("XPC_SERVICE_NAME", "")
	t.Setenv("INVOCATION_ID", "")
}

func skipOnWindows(t *testing.T, why string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip(why)
	}
}

func readPlist(t *testing.T) string {
	t.Helper()
	path, _ := launchdPath()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func readService(t *testing.T) string {
	t.Helper()
	dir, _ := systemdDir()
	b, err := os.ReadFile(filepath.Join(dir, systemdUnit+".service"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestMigrateNothingInstalled(t *testing.T) {
	for _, platform := range []string{"darwin", "linux", "windows"} {
		calls := fakeOS(t, platform)
		var out strings.Builder
		if err := Migrate(&out); err != nil || out.Len() != 0 || len(*calls) != 0 {
			t.Errorf("%s: err=%v out=%q calls=%v", platform, err, out.String(), *calls)
		}
	}
}

// The OS job runs the old alias, so the first migration has to work when this
// process *is* the alias: delistd is found next to it, not on launchd's PATH.
func TestMigrateLaunchdFromTheAliasRun(t *testing.T) {
	calls := fakeOS(t, "darwin")
	notInOSJob(t)
	bin := t.TempDir()
	binaries(t, bin, "eraser", "delistd")
	installFakeSchedule(t, "darwin", filepath.Join(bin, "eraser"))
	runningAs(t, filepath.Join(bin, "eraser"))

	var out strings.Builder
	if err := Migrate(&out); err != nil {
		t.Fatal(err)
	}
	plist := readPlist(t)
	if !strings.Contains(plist, "<string>"+filepath.Join(bin, "delistd")+"</string>") || strings.Contains(plist, filepath.Join(bin, "eraser")) {
		t.Errorf("plist not pointed at delistd:\n%s", plist)
	}
	for _, keep := range []string{"<string>auto</string>", "<string>--once</string>", "<string>/home/user/.eraser/config.yaml</string>", launchdLabel} {
		if !strings.Contains(plist, keep) {
			t.Errorf("plist lost %q:\n%s", keep, plist)
		}
	}
	if !strings.Contains(out.String(), "delistd") || !strings.Contains(out.String(), "eraser") {
		t.Errorf("notice = %q", out.String())
	}
	got := strings.Join(*calls, "\n")
	if !strings.Contains(got, "launchctl bootout") || !strings.Contains(got, "launchctl bootstrap") {
		t.Errorf("launchd not reloaded: %q", got)
	}
}

// Unloading the agent kills its running job, which from the scheduled run is
// this process, so the reload must be deferred to a detached helper.
func TestMigrateLaunchdInsideTheJobDefersTheReload(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("launchd isn't on windows")
	}
	calls := fakeOS(t, "darwin")
	t.Setenv("XPC_SERVICE_NAME", launchdLabel)
	bin := t.TempDir()
	binaries(t, bin, "eraser", "delistd")
	installFakeSchedule(t, "darwin", filepath.Join(bin, "eraser"))
	runningAs(t, filepath.Join(bin, "eraser"))

	if err := Migrate(nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(readPlist(t), filepath.Join(bin, "delistd")) {
		t.Error("plist not rewritten")
	}
	if len(*calls) != 1 || !strings.HasPrefix((*calls)[0], "/bin/sh -c ") {
		t.Fatalf("want exactly one detached helper and no direct launchctl call, got %q", *calls)
	}
	script := (*calls)[0]
	for _, want := range []string{"kill -0", "launchctl bootout", "launchctl bootstrap", "'gui/"} {
		if !strings.Contains(script, want) {
			t.Errorf("helper script missing %q: %s", want, script)
		}
	}
}

func TestMigrateLaunchdLeavesWhatItCannotImprove(t *testing.T) {
	notInOSJob(t)
	for name, setup := range map[string]func(bin string){
		"already delistd": func(bin string) {
			binaries(t, bin, "delistd")
			installFakeSchedule(t, "darwin", filepath.Join(bin, "delistd"))
		},
		"no delistd beside eraser, running as eraser": func(bin string) {
			binaries(t, bin, "eraser")
			installFakeSchedule(t, "darwin", filepath.Join(bin, "eraser"))
			runningAs(t, filepath.Join(bin, "eraser"))
		},
		"a custom executable": func(bin string) {
			binaries(t, bin, "mytool", "delistd")
			installFakeSchedule(t, "darwin", filepath.Join(bin, "mytool"))
		},
	} {
		t.Run(name, func(t *testing.T) {
			calls := fakeOS(t, "darwin")
			notInOSJob(t)
			bin := t.TempDir()
			setup(bin)
			before := readPlist(t)
			var out strings.Builder
			if err := Migrate(&out); err != nil {
				t.Fatal(err)
			}
			if readPlist(t) != before || out.Len() != 0 || len(*calls) != 0 {
				t.Errorf("should be untouched: out=%q calls=%v", out.String(), *calls)
			}
		})
	}
}

// With no delistd beside the old binary, a real delistd can still take over.
func TestMigrateLaunchdUsesThisBinaryWhenItIsDelistd(t *testing.T) {
	skipOnWindows(t, "launchd and its path handling aren't on windows")
	fakeOS(t, "darwin")
	notInOSJob(t)
	elsewhere := outsideTMPDIR(t)
	binaries(t, elsewhere, "delistd")
	oldDir := t.TempDir()
	binaries(t, oldDir, "eraser")
	installFakeSchedule(t, "darwin", filepath.Join(oldDir, "eraser"))
	runningAs(t, filepath.Join(elsewhere, "delistd"))

	if err := Migrate(nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(readPlist(t), filepath.Join(elsewhere, "delistd")) {
		t.Errorf("not migrated to the running delistd:\n%s", readPlist(t))
	}
}

func TestMigrateIsIdempotent(t *testing.T) {
	calls := fakeOS(t, "darwin")
	notInOSJob(t)
	bin := t.TempDir()
	binaries(t, bin, "eraser", "delistd")
	installFakeSchedule(t, "darwin", filepath.Join(bin, "eraser"))
	runningAs(t, filepath.Join(bin, "delistd"))

	if err := Migrate(nil); err != nil {
		t.Fatal(err)
	}
	first, n := readPlist(t), len(*calls)
	var out strings.Builder
	if err := Migrate(&out); err != nil {
		t.Fatal(err)
	}
	if readPlist(t) != first || len(*calls) != n || out.Len() != 0 {
		t.Errorf("second run changed something: out=%q calls=%v", out.String(), (*calls)[n:])
	}
}

// A path with XML-significant characters must stay well-formed and round-trip.
func TestMigrateLaunchdEscapesThePath(t *testing.T) {
	skipOnWindows(t, "windows doesn't allow < or > in a file name")
	fakeOS(t, "darwin")
	notInOSJob(t)
	bin := filepath.Join(t.TempDir(), "R&D <tools>")
	binaries(t, bin, "eraser", "delistd")
	installFakeSchedule(t, "darwin", filepath.Join(bin, "eraser"))
	runningAs(t, filepath.Join(bin, "eraser"))

	if err := Migrate(nil); err != nil {
		t.Fatal(err)
	}
	plist := readPlist(t)
	if err := xml.Unmarshal([]byte(plist), new(struct{})); err != nil {
		t.Fatalf("plist no longer parses: %v\n%s", err, plist)
	}
	if strings.Contains(plist, "R&D <") || !strings.Contains(plist, "R&amp;D &lt;tools&gt;") {
		t.Errorf("path not escaped:\n%s", plist)
	}
	// and it can be read back for a later run
	if m := plistExeRe.FindStringSubmatch(plist); m == nil || xmlUnescape(m[1]) != filepath.Join(bin, "delistd") {
		t.Errorf("round trip failed: %v", m)
	}
}

func TestMigrateSystemd(t *testing.T) {
	for _, inJob := range []bool{false, true} {
		calls := fakeOS(t, "linux")
		if inJob {
			t.Setenv("INVOCATION_ID", "abc") // systemd sets this for the service
		} else {
			notInOSJob(t)
		}
		bin := filepath.Join(t.TempDir(), "100%")
		binaries(t, bin, "eraser", "delistd")
		installFakeSchedule(t, "linux", filepath.Join(bin, "eraser"))
		runningAs(t, filepath.Join(bin, "eraser"))

		var out strings.Builder
		if err := Migrate(&out); err != nil {
			t.Fatal(err)
		}
		svc := readService(t)
		want := `ExecStart="` + systemdEscape(filepath.Join(bin, "delistd")) + `" "auto" "--once"`
		if !strings.Contains(svc, want) || strings.Contains(svc, "eraser") && strings.Contains(svc, filepath.Join(bin, "eraser")) {
			t.Errorf("inJob=%v service:\n%s\nwant to contain %s", inJob, svc, want)
		}
		if !strings.Contains(svc, "%%") {
			t.Errorf("percent sign not escaped for systemd:\n%s", svc)
		}
		if !strings.Contains(strings.Join(*calls, "\n"), "systemctl --user daemon-reload") || out.Len() == 0 {
			t.Errorf("inJob=%v calls=%v out=%q", inJob, *calls, out.String())
		}
		if strings.Contains(strings.Join(*calls, "\n"), "/bin/sh") {
			t.Errorf("systemd needs no deferred reload: %v", *calls)
		}
	}
}

func TestMigrateSystemdTimerWithoutService(t *testing.T) {
	fakeOS(t, "linux")
	notInOSJob(t)
	dir, _ := systemdDir()
	if err := writeFile(filepath.Join(dir, systemdUnit+".timer"), "[Timer]\n"); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(nil); err != nil {
		t.Errorf("timer without a service: %v", err)
	}
}

// The file is rewritten before the reload, so a failed reload still leaves the
// schedule migrated: say so, and the next run won't try (or warn) again.
func TestMigrateReloadFailuresAreReportedButKept(t *testing.T) {
	t.Run("launchd", func(t *testing.T) {
		fakeOS(t, "darwin", "bootstrap")
		notInOSJob(t)
		bin := t.TempDir()
		binaries(t, bin, "eraser", "delistd")
		installFakeSchedule(t, "darwin", filepath.Join(bin, "eraser"))
		runningAs(t, filepath.Join(bin, "eraser"))
		err := Migrate(nil)
		if err == nil || !strings.Contains(err.Error(), "could not reload launchd") || !strings.Contains(readPlist(t), filepath.Join(bin, "delistd")) {
			t.Errorf("err=%v", err)
		}
		if err := Migrate(nil); err != nil {
			t.Errorf("second run should be a quiet no-op, got %v", err)
		}
	})
	t.Run("systemd", func(t *testing.T) {
		fakeOS(t, "linux", "daemon-reload")
		notInOSJob(t)
		bin := t.TempDir()
		binaries(t, bin, "eraser", "delistd")
		installFakeSchedule(t, "linux", filepath.Join(bin, "eraser"))
		runningAs(t, filepath.Join(bin, "eraser"))
		if err := Migrate(nil); err == nil || !strings.Contains(err.Error(), "daemon-reload failed") {
			t.Errorf("err=%v", err)
		}
	})
}

func TestWriteFileAtomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.plist")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeFileAtomic(path, "new"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(path); string(b) != "new" {
		t.Errorf("content = %q", b)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("temp file left behind: %v", entries)
	}
	if err := writeFileAtomic(filepath.Join(dir, "missing", "x"), "y"); err == nil {
		t.Error("a missing directory should fail")
	}
}

func TestEscapesRoundTrip(t *testing.T) {
	for _, s := range []string{`/usr/local/bin/delistd`, `/Users/R&D/<x>/bin`, `/odd "quoted" 100%/path\x`} {
		if got := xmlUnescape(xmlEscape(s)); got != s {
			t.Errorf("xml round trip %q -> %q", s, got)
		}
		if got := systemdUnescape(systemdEscape(s)); got != s {
			t.Errorf("systemd round trip %q -> %q", s, got)
		}
	}
}

func TestMigrateIgnoresFilesItDoesNotRecognise(t *testing.T) {
	notInOSJob(t)
	t.Run("plist without ProgramArguments", func(t *testing.T) {
		fakeOS(t, "darwin")
		notInOSJob(t)
		path, _ := launchdPath()
		if err := writeFile(path, "<plist><dict/></plist>"); err != nil {
			t.Fatal(err)
		}
		if err := Migrate(nil); err != nil || readPlist(t) != "<plist><dict/></plist>" {
			t.Errorf("err=%v", err)
		}
	})
	t.Run("service without ExecStart", func(t *testing.T) {
		fakeOS(t, "linux")
		notInOSJob(t)
		dir, _ := systemdDir()
		for name, body := range map[string]string{".timer": "[Timer]\n", ".service": "[Service]\nType=oneshot\n"} {
			if err := writeFile(filepath.Join(dir, systemdUnit+name), body); err != nil {
				t.Fatal(err)
			}
		}
		if err := Migrate(nil); err != nil || readService(t) != "[Service]\nType=oneshot\n" {
			t.Errorf("err=%v", err)
		}
	})
}

func TestMigrateReportsAnUnreadableSchedule(t *testing.T) {
	fakeOS(t, "darwin")
	notInOSJob(t)
	path, _ := launchdPath()
	if err := os.MkdirAll(path, 0o755); err != nil { // present (so Installed) but not a readable file
		t.Fatal(err)
	}
	if err := Migrate(nil); err == nil {
		t.Error("an unreadable schedule should be reported, not skipped silently")
	}
}

func TestXMLUnescapeKeepsWhatItCannotParse(t *testing.T) {
	if got := xmlUnescape("a &bogus; b"); got != "a &bogus; b" {
		t.Errorf("got %q", got)
	}
}

func TestMigrateReportsASystemdServiceItCannotRead(t *testing.T) {
	fakeOS(t, "linux")
	notInOSJob(t)
	dir, _ := systemdDir()
	if err := writeFile(filepath.Join(dir, systemdUnit+".timer"), "[Timer]\n"); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, systemdUnit+".service"), 0o755); err != nil { // a directory, not a file
		t.Fatal(err)
	}
	if err := Migrate(nil); err == nil {
		t.Error("an unreadable service file should be reported")
	}
}

// If the rewrite can't be written, say so and leave the old schedule as it was.
func TestMigrateReportsAScheduleItCannotWrite(t *testing.T) {
	skipOnWindows(t, "directory permissions work differently on windows")
	if os.Geteuid() == 0 {
		t.Skip("root can write anywhere")
	}
	for _, platform := range []string{"darwin", "linux"} {
		fakeOS(t, platform)
		notInOSJob(t)
		bin := t.TempDir()
		binaries(t, bin, "eraser", "delistd")
		installFakeSchedule(t, platform, filepath.Join(bin, "eraser"))
		runningAs(t, filepath.Join(bin, "eraser"))
		var dir string
		if platform == "darwin" {
			p, _ := launchdPath()
			dir = filepath.Dir(p)
		} else {
			dir, _ = systemdDir()
		}
		if err := os.Chmod(dir, 0o500); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
		if err := Migrate(nil); err == nil || !strings.Contains(err.Error(), "failed to write") {
			t.Errorf("%s: err=%v", platform, err)
		}
	}
}
