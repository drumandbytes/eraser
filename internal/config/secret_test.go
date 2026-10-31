package config

import (
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
