package config

import (
	"errors"
	"os"
	"strings"
	"testing"
)

func withMachineID(t *testing.T, id string) {
	t.Helper()
	old := machineID
	machineID = func() (string, error) { return id, nil }
	t.Cleanup(func() { machineID = old })
}

const secretCfg = `profiles:
  - id: default
    first_name: A
    last_name: B
    email: a@b.co
    mail:
      email:
        from: a@b.co
        smtp: {host: smtp.example.com, port: 587, username: u, password: profile-smtp-secret}
      inbox:
        enabled: true
        server: imap.example.com
        port: 993
        email: a@b.co
        password: profile-imap-secret
email:
  from: a@b.co
  smtp: {host: smtp.example.com, port: 587, username: u, password: shared-smtp-secret}
inbox:
  enabled: true
  server: imap.example.com
  port: 993
  email: a@b.co
  password: shared-imap-secret
`

var allSecrets = []string{"profile-smtp-secret", "profile-imap-secret", "shared-smtp-secret", "shared-imap-secret"}

func TestPasswordsAreEncryptedOnDiskAndRoundTrip(t *testing.T) {
	withMachineID(t, "machine-one")
	p := writeCfg(t, "profiles:\n  - id: default\n    first_name: A\n    last_name: B\n    email: a@b.co\n")
	cfg, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Email.SMTP.Password = "hunter2"
	cfg.Inbox.Password = "imap-hunter2"
	if err := Save(p, cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Email.SMTP.Password != "hunter2" {
		t.Error("Save must not change the live config")
	}

	raw, _ := os.ReadFile(p)
	if strings.Contains(string(raw), "hunter2") || strings.Count(string(raw), secretPrefix) != 2 {
		t.Fatalf("passwords not encrypted on disk:\n%s", raw)
	}
	again, err := Load(p)
	if err != nil || again.Email.SMTP.Password != "hunter2" || again.Inbox.Password != "imap-hunter2" {
		t.Fatalf("round trip: %v %q %q", err, again.Email.SMTP.Password, again.Inbox.Password)
	}
}

func TestPlaintextPasswordsMigrateAndBackupHasNone(t *testing.T) {
	withMachineID(t, "machine-one")
	p := writeCfg(t, secretCfg)
	cfg, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Email.SMTP.Password != "shared-smtp-secret" || cfg.Profiles[0].Mail.Inbox.Password != "profile-imap-secret" {
		t.Fatal("passwords must be usable in memory")
	}
	for name, path := range map[string]string{"config": p, "backup": p + ".1x.bak"} {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for _, secret := range allSecrets {
			if strings.Contains(string(raw), secret) {
				t.Errorf("%s still contains %q", name, secret)
			}
		}
	}
	if raw, _ := os.ReadFile(p); strings.Count(string(raw), secretPrefix) != 4 {
		t.Errorf("want 4 encrypted passwords:\n%s", raw)
	}
	if _, err := Load(p); err != nil {
		t.Fatalf("second load: %v", err)
	}
}

func TestPasswordFromAnotherMachineIsNotDecrypted(t *testing.T) {
	withMachineID(t, "machine-one")
	p := writeCfg(t, "email:\n  from: a@b.co\n  smtp: {host: h, port: 587, username: u, password: pw}\n")
	if _, err := Load(p); err != nil {
		t.Fatal(err)
	}

	withMachineID(t, "machine-two")
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("an undecryptable password must not fail the load: %v", err)
	}
	if cfg.Email.SMTP.Password != "" {
		t.Errorf("got %q on a different machine", cfg.Email.SMTP.Password)
	}
}

func TestSecretEdgeCases(t *testing.T) {
	withMachineID(t, "m")
	if got, err := encryptSecret(""); got != "" || err != nil {
		t.Errorf("empty: %q %v", got, err)
	}
	enc, _ := encryptSecret("x")
	if again, _ := encryptSecret(enc); again != enc {
		t.Error("encrypting twice must be a no-op")
	}
	if a, _ := encryptSecret("x"); a == enc {
		t.Error("nonce must differ per encryption")
	}
	if got, err := decryptSecret("legacy"); got != "legacy" || err != nil {
		t.Errorf("plaintext passthrough: %q %v", got, err)
	}
	for _, bad := range []string{secretPrefix + "!!!", secretPrefix + "AAAA"} {
		if _, err := decryptSecret(bad); err == nil {
			t.Errorf("%q should be rejected", bad)
		}
	}
	if maskPasswords([]byte("a: [unclosed")) != nil {
		t.Error("unparseable document must yield no backup")
	}
}

func TestRealMachineIDKey(t *testing.T) {
	// No override: whatever this platform provides (or the hostname fallback) must work.
	enc, err := encryptSecret("pw")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := decryptSecret(enc); err != nil || got != "pw" {
		t.Errorf("round trip with the real machine id: %q %v", got, err)
	}
}

// resetPassphrase clears the process-wide passphrase state around a test.
func resetPassphrase(t *testing.T, provider func() (string, error)) {
	t.Helper()
	oldProvider := PassphraseProvider
	SetPassphrase("")
	PassphraseProvider = provider
	t.Cleanup(func() { PassphraseProvider = oldProvider; SetPassphrase("") })
}

func baseSecretConfig(t *testing.T) (string, *Config) {
	t.Helper()
	p := writeCfg(t, "profiles:\n  - id: default\n    first_name: A\n    last_name: B\n    email: a@b.co\nemail:\n  from: a@b.co\n  smtp: {host: h, port: 587, username: u, password: smtp-pw}\ninbox:\n  enabled: true\n  server: i\n  port: 993\n  email: a@b.co\n  password: imap-pw\n")
	cfg, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	return p, cfg
}

func TestPassphraseProtectsPasswordsAndRoundTrips(t *testing.T) {
	withMachineID(t, "m1")
	resetPassphrase(t, nil)
	p, cfg := baseSecretConfig(t)

	if err := cfg.SetPassphraseProtection("correct horse"); err != nil {
		t.Fatal(err)
	}
	if err := Save(p, cfg); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(p)
	if strings.Contains(string(raw), "smtp-pw") || strings.Count(string(raw), secretPrefixV2) != 2 || !strings.Contains(string(raw), "passphrase_protected: true") {
		t.Fatalf("expected two v2 secrets and the flag:\n%s", raw)
	}

	// A fresh process: Load works without the passphrase, secrets stay opaque.
	SetPassphrase("")
	loaded, err := Load(p)
	if err != nil || !loaded.PassphraseProtected {
		t.Fatalf("load = %v, protected=%v", err, loaded != nil && loaded.PassphraseProtected)
	}
	if _, err := Reveal(loaded.Email.SMTP.Password); !errors.Is(err, ErrLocked) {
		t.Fatalf("no provider: want ErrLocked, got %v", err)
	}

	asked := 0
	PassphraseProvider = func() (string, error) { asked++; return "correct horse", nil }
	if got, err := Reveal(loaded.Email.SMTP.Password); err != nil || got != "smtp-pw" {
		t.Fatalf("Reveal = %q, %v", got, err)
	}
	if got, _ := Reveal(loaded.Inbox.Password); got != "imap-pw" || asked != 1 {
		t.Errorf("second reveal = %q, prompts = %d (want 1)", got, asked)
	}
}

func TestPassphraseWrongThenRightAndUnlock(t *testing.T) {
	withMachineID(t, "m1")
	resetPassphrase(t, nil)
	p, cfg := baseSecretConfig(t)
	if err := cfg.SetPassphraseProtection("right-pass-1"); err != nil {
		t.Fatal(err)
	}
	if err := Save(p, cfg); err != nil {
		t.Fatal(err)
	}
	loaded, _ := Load(p)

	SetPassphrase("")
	answers := []string{"wrong-1", "wrong-2", "right-pass-1"}
	PassphraseProvider = func() (string, error) {
		a := answers[0]
		answers = answers[1:]
		return a, nil
	}
	if err := loaded.Unlock(); err != nil {
		t.Fatalf("Unlock with the third try right: %v", err)
	}

	SetPassphrase("")
	PassphraseProvider = func() (string, error) { return "nope", nil }
	if err := loaded.Unlock(); err == nil || !strings.Contains(err.Error(), "wrong passphrase") {
		t.Errorf("three wrong tries should fail, got %v", err)
	}
	plain := &Config{}
	if err := plain.Unlock(); err != nil {
		t.Errorf("no passphrase set: Unlock must be a no-op, got %v", err)
	}
}

func TestPassphraseNewPasswordsAndClear(t *testing.T) {
	withMachineID(t, "m1")
	resetPassphrase(t, nil)
	p, cfg := baseSecretConfig(t)
	if err := cfg.SetPassphraseProtection("right-pass-1"); err != nil {
		t.Fatal(err)
	}
	if err := Save(p, cfg); err != nil {
		t.Fatal(err)
	}

	// A password typed in later (web form, init) is encrypted under the passphrase too.
	loaded, _ := Load(p)
	loaded.Email.SMTP.Password = "new-smtp-pw"
	if err := Save(p, loaded); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(p)
	if strings.Contains(string(raw), "new-smtp-pw") || strings.Count(string(raw), secretPrefixV2) != 2 {
		t.Fatalf("new password not protected:\n%s", raw)
	}

	// Clearing goes back to machine-bound v1 and keeps every password.
	again, _ := Load(p)
	if err := again.SetPassphraseProtection(""); err != nil {
		t.Fatal(err)
	}
	if err := Save(p, again); err != nil {
		t.Fatal(err)
	}
	SetPassphrase("")
	final, err := Load(p)
	if err != nil || final.PassphraseProtected || final.Email.SMTP.Password != "new-smtp-pw" || final.Inbox.Password != "imap-pw" {
		t.Fatalf("after clear: %v %+v", err, final.Email.SMTP)
	}
	raw, _ = os.ReadFile(p)
	if strings.Contains(string(raw), secretPrefixV2) || strings.Contains(string(raw), "passphrase_protected") {
		t.Errorf("v2 leftovers after clear:\n%s", raw)
	}
}

func TestSavingProtectedConfigWithoutPassphraseFails(t *testing.T) {
	withMachineID(t, "m1")
	resetPassphrase(t, nil)
	p, cfg := baseSecretConfig(t)
	cfg.PassphraseProtected = true
	cfg.Email.SMTP.Password = "plain-new"
	if err := Save(p, cfg); err == nil || !errors.Is(err, ErrLocked) {
		t.Fatalf("want ErrLocked, got %v", err)
	}
}

func TestV2CorruptInputs(t *testing.T) {
	for _, bad := range []string{secretPrefixV2 + "!!!", secretPrefixV2 + "AAAA", secretPrefixV2 + "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=="} {
		if _, err := decryptV2(bad, "x"); err == nil {
			t.Errorf("%q should be rejected", bad)
		}
	}
}

func TestMachineIDFallsBackToHostname(t *testing.T) {
	old := machineID
	t.Cleanup(func() { machineID = old })
	for name, fn := range map[string]func() (string, error){
		"error":    func() (string, error) { return "", errors.New("no id") },
		"empty id": func() (string, error) { return "", nil },
	} {
		machineID = fn
		enc, err := encryptSecret("pw")
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got, err := decryptSecret(enc); err != nil || got != "pw" {
			t.Errorf("%s: round trip = %q, %v", name, got, err)
		}
	}
}

func TestDecryptSecretRejectsBadCiphertext(t *testing.T) {
	withMachineID(t, "m")
	for _, bad := range []string{secretPrefix + "!!!", secretPrefix + "AAAA", secretPrefix + "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"} {
		if _, err := decryptSecret(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	enc, _ := encryptSecret("pw")
	withMachineID(t, "another machine")
	if _, err := decryptSecret(enc); err == nil {
		t.Error("a password from another machine must not decrypt")
	}
}

func TestPassphraseProviderFailureMeansLocked(t *testing.T) {
	withMachineID(t, "m")
	resetPassphrase(t, func() (string, error) { return "", errors.New("not a terminal") })
	enc, err := encryptV2("pw", "right-pass-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Reveal(enc); !errors.Is(err, ErrLocked) {
		t.Errorf("provider failure should read as locked, got %v", err)
	}
	if PassphraseUnlocked() {
		t.Error("nothing was entered")
	}
}

func TestRevealWrongPassphraseForgetsIt(t *testing.T) {
	withMachineID(t, "m")
	resetPassphrase(t, nil)
	enc, _ := encryptV2("pw", "right-pass-1")
	SetPassphrase("wrong-pass-2")
	if _, err := Reveal(enc); err == nil || PassphraseUnlocked() {
		t.Errorf("a wrong passphrase must fail and be forgotten (err=%v unlocked=%v)", err, PassphraseUnlocked())
	}
	SetPassphrase("right-pass-1")
	if got, err := Reveal(enc); err != nil || got != "pw" || !PassphraseUnlocked() {
		t.Errorf("Reveal = %q, %v", got, err)
	}
	if got, _ := Reveal("plain"); got != "plain" {
		t.Error("a value that isn't v2 passes through")
	}
}

func TestCheckPassphraseDoesNotTouchTheCache(t *testing.T) {
	withMachineID(t, "m")
	resetPassphrase(t, nil)
	_, cfg := baseSecretConfig(t)
	if err := cfg.CheckPassphrase("anything"); err != nil {
		t.Errorf("no passphrase set: %v", err)
	}
	if err := cfg.SetPassphraseProtection("right-pass-1"); err != nil {
		t.Fatal(err)
	}
	p := writeCfg(t, "")
	if err := Save(p, cfg); err != nil {
		t.Fatal(err)
	}
	stored, _ := Load(p)
	SetPassphrase("")
	if err := stored.CheckPassphrase("wrong-pass-2"); err == nil {
		t.Error("wrong passphrase accepted")
	}
	if err := stored.CheckPassphrase("right-pass-1"); err != nil {
		t.Errorf("right passphrase rejected: %v", err)
	}
	if PassphraseUnlocked() {
		t.Error("CheckPassphrase must not unlock the process")
	}
}

func TestCloneIsolatesPasswords(t *testing.T) {
	cfg := &Config{
		Email: EmailConfig{SMTP: SMTPConfig{Password: "a"}},
		Profiles: []NamedProfile{{ID: "p", Mail: &MailConfig{
			Email: &EmailConfig{SMTP: SMTPConfig{Password: "b"}},
			Inbox: &InboxConfig{Password: "c"},
		}}},
	}
	clone := cfg.Clone()
	clone.Profiles[0].Mail.Email.SMTP.Password = "changed"
	clone.Profiles[0].Mail.Inbox.Password = "changed"
	if cfg.Profiles[0].Mail.Email.SMTP.Password != "b" || cfg.Profiles[0].Mail.Inbox.Password != "c" {
		t.Error("changing the clone changed the original")
	}
	if err := (&Config{}).secretFields(func(*string) error { return errors.New("stop") }); err == nil {
		t.Error("an error from the callback must stop the walk")
	}
}

func TestSaveFailsWhenTheDirectoryCannotBeCreated(t *testing.T) {
	file := writeCfg(t, "x") // a file, so nothing can be created under it
	if err := Save(file+"/sub/config.yaml", &Config{}); err == nil {
		t.Error("expected a directory error")
	}
}
