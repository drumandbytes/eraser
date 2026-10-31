package main

import (
	"errors"
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/drumandbytes/eraser/internal/config"
)

func scriptedSecrets(t *testing.T, answers ...string) {
	t.Helper()
	old := readSecret
	readSecret = func(string) (string, error) {
		a := answers[0]
		answers = answers[1:]
		return a, nil
	}
	config.SetPassphrase("")
	t.Cleanup(func() { readSecret = old; config.SetPassphrase("") })
}

const passphraseTestConfig = `profiles:
  - id: default
    first_name: Jane
    last_name: Doe
    email: jane@example.com
email:
  from: jane@example.com
  smtp: {host: smtp.example.com, port: 587, username: jane, password: smtp-secret}
options:
  template: gdpr
`

func TestPassphraseSetAndClear(t *testing.T) {
	e := newCLIEnv(t, passphraseTestConfig)

	scriptedSecrets(t, "long-enough-pass", "long-enough-pass")
	out, err := e.run(t, "", "passphrase", "set", "--yes")
	if err != nil {
		t.Fatalf("set: %v\n%s", err, out)
	}
	mustContain(t, out, "Heads-up", "can't unlock your mail password", "schedule install", "now protected")
	raw, _ := os.ReadFile(e.cfgPath)
	if !strings.Contains(string(raw), "enc:v2:") || strings.Contains(string(raw), "smtp-secret") || !strings.Contains(string(raw), "passphrase_protected: true") {
		t.Fatalf("config not passphrase protected:\n%s", raw)
	}

	scriptedSecrets(t, "long-enough-pass") // clear asks for the current passphrase
	out, err = e.run(t, "", "passphrase", "clear")
	if err != nil {
		t.Fatalf("clear: %v\n%s", err, out)
	}
	mustContain(t, out, "Passphrase removed")
	raw, _ = os.ReadFile(e.cfgPath)
	if strings.Contains(string(raw), "enc:v2:") || strings.Contains(string(raw), "passphrase_protected") || !strings.Contains(string(raw), "enc:v1:") {
		t.Fatalf("not back to machine-bound:\n%s", raw)
	}
	out, _ = e.run(t, "", "passphrase", "clear")
	mustContain(t, out, "No passphrase is set")
}

func TestPassphraseSetRejections(t *testing.T) {
	e := newCLIEnv(t, passphraseTestConfig)
	scriptedSecrets(t, "short")
	if _, err := e.run(t, "", "passphrase", "set", "--yes"); err == nil || !strings.Contains(err.Error(), "at least") {
		t.Errorf("short passphrase: %v", err)
	}
	scriptedSecrets(t, "long-enough-pass", "different-pass-x")
	if _, err := e.run(t, "", "passphrase", "set", "--yes"); err == nil || !strings.Contains(err.Error(), "don't match") {
		t.Errorf("mismatch: %v", err)
	}
	out, err := e.run(t, "n\n", "passphrase", "set")
	if err != nil {
		t.Fatal(err)
	}
	mustContain(t, out, "Nothing changed")
	if raw, _ := os.ReadFile(e.cfgPath); strings.Contains(string(raw), "enc:v2:") {
		t.Error("declining must not change the config")
	}
}

func TestPassphraseProviderAsksOnTheTerminal(t *testing.T) {
	scriptedSecrets(t, "typed passphrase")
	got, err := config.PassphraseProvider()
	if err != nil || got != "typed passphrase" {
		t.Errorf("provider = %q, %v", got, err)
	}
}

func TestPassphraseCommandsFailCleanly(t *testing.T) {
	noConfig := newCLIEnv(t, "")
	for _, args := range [][]string{{"passphrase", "set", "--yes"}, {"passphrase", "clear"}} {
		if _, err := noConfig.run(t, "", args...); err == nil || !strings.Contains(err.Error(), "failed to load config") {
			t.Errorf("%v without a config: %v", args, err)
		}
	}

	e := newCLIEnv(t, passphraseTestConfig)
	old := readSecret
	t.Cleanup(func() { readSecret = old })
	readSecret = func(string) (string, error) { return "", errors.New("not a terminal") }
	if _, err := e.run(t, "", "passphrase", "set", "--yes"); err == nil || !strings.Contains(err.Error(), "can't read the passphrase") {
		t.Errorf("no terminal: %v", err)
	}

	// The repeat prompt failing is reported too.
	calls := 0
	readSecret = func(string) (string, error) {
		calls++
		if calls == 2 {
			return "", errors.New("closed")
		}
		return "long-enough-pass", nil
	}
	if _, err := e.run(t, "", "passphrase", "set", "--yes"); err == nil {
		t.Error("a failing repeat prompt must fail the command")
	}

	// Clearing with the wrong passphrase leaves the config alone.
	scriptedSecrets(t, "long-enough-pass", "long-enough-pass")
	if _, err := e.run(t, "", "passphrase", "set", "--yes"); err != nil {
		t.Fatal(err)
	}
	scriptedSecrets(t, "wrong-1", "wrong-2", "wrong-3")
	if _, err := e.run(t, "", "passphrase", "clear"); err == nil {
		t.Error("clear with the wrong passphrase must fail")
	}
	if raw, _ := os.ReadFile(e.cfgPath); !strings.Contains(string(raw), "enc:v2:") {
		t.Error("a failed clear must leave the passphrase in place")
	}
}

func skipWithoutPermissions(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs POSIX permissions and a non-root user")
	}
}

func TestPassphraseSetWhenTheCurrentOneCannotBeEntered(t *testing.T) {
	e := newCLIEnv(t, passphraseTestConfig)
	scriptedSecrets(t, "long-enough-pass", "long-enough-pass")
	if _, err := e.run(t, "", "passphrase", "set", "--yes"); err != nil {
		t.Fatal(err)
	}

	// Changing it needs the old one to decrypt first; the third prompt fails.
	calls := 0
	old := readSecret
	t.Cleanup(func() { readSecret = old; config.SetPassphrase("") })
	config.SetPassphrase("")
	readSecret = func(string) (string, error) {
		calls++
		if calls <= 2 {
			return "another-long-pass", nil
		}
		return "", errors.New("not a terminal")
	}
	if _, err := e.run(t, "", "passphrase", "set", "--yes"); err == nil || !errors.Is(err, config.ErrLocked) {
		t.Errorf("want the old passphrase to be required, got %v", err)
	}
}

func TestPassphraseCommandsReportASaveFailure(t *testing.T) {
	skipWithoutPermissions(t)
	e := newCLIEnv(t, passphraseTestConfig)

	if err := os.Chmod(e.cfgPath, 0o400); err != nil {
		t.Fatal(err)
	}
	scriptedSecrets(t, "long-enough-pass", "long-enough-pass")
	if _, err := e.run(t, "", "passphrase", "set", "--yes"); err == nil || !strings.Contains(err.Error(), "failed to save config") {
		t.Errorf("set on a read-only config: %v", err)
	}

	if err := os.Chmod(e.cfgPath, 0o600); err != nil {
		t.Fatal(err)
	}
	scriptedSecrets(t, "long-enough-pass", "long-enough-pass")
	if _, err := e.run(t, "", "passphrase", "set", "--yes"); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(e.cfgPath, 0o400); err != nil {
		t.Fatal(err)
	}
	scriptedSecrets(t, "long-enough-pass")
	if _, err := e.run(t, "", "passphrase", "clear"); err == nil || !strings.Contains(err.Error(), "failed to save config") {
		t.Errorf("clear on a read-only config: %v", err)
	}
}

func TestAutoLoopAndServeNeedThePassphrase(t *testing.T) {
	e := newCLIEnv(t, passphraseTestConfig)
	scriptedSecrets(t, "long-enough-pass", "long-enough-pass")
	if _, err := e.run(t, "", "passphrase", "set", "--yes"); err != nil {
		t.Fatal(err)
	}
	// Wrong three times: the loop refuses to start instead of stalling later.
	scriptedSecrets(t, "wrong-one", "wrong-two", "wrong-three")
	if _, err := e.run(t, "", "auto"); err == nil || !strings.Contains(err.Error(), "wrong passphrase") {
		t.Errorf("auto with the wrong passphrase: %v", err)
	}
}
