package main

import (
	"os"
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
