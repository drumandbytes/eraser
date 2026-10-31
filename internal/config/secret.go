package config

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"os/user"
	"strings"
)

// Passwords are stored in config.yaml as "enc:v1:<base64(nonce||ciphertext)>",
// AES-256-GCM under a key derived from this machine's ID and the current
// user. A config copied to another machine (backup, dotfile sync, a pasted
// file) can't be decrypted there; someone logged in as you on this machine
// still can, as they could by running Eraser. That is the trade for sending
// unattended without a passphrase or keychain.
const secretPrefix = "enc:v1:"

// machineID returns a stable per-machine identifier. Replaceable in tests.
var machineID = readMachineID

func secretKey() ([]byte, error) {
	id, err := machineID()
	if err != nil || id == "" {
		host, herr := os.Hostname()
		if herr != nil {
			return nil, errors.New("no machine identifier available to protect passwords")
		}
		id = "host:" + host
	}
	who := "unknown"
	if u, err := user.Current(); err == nil {
		who = u.Uid + ":" + u.Username
	}
	return hkdf.Key(sha256.New, []byte(id+"\x00"+who), []byte("eraser/config-secret/v1"), "aes-256-gcm", 32)
}

func newGCM() (cipher.AEAD, error) {
	key, err := secretKey()
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func isEncrypted(s string) bool { return strings.HasPrefix(s, secretPrefix) }

// encryptSecret leaves "" and already-encrypted values alone.
func encryptSecret(plain string) (string, error) {
	if plain == "" || isEncrypted(plain) {
		return plain, nil
	}
	gcm, err := newGCM()
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	return secretPrefix + base64.StdEncoding.EncodeToString(gcm.Seal(nonce, nonce, []byte(plain), nil)), nil
}

// decryptSecret returns plaintext values (legacy configs) unchanged.
func decryptSecret(s string) (string, error) {
	if !isEncrypted(s) {
		return s, nil
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(s, secretPrefix))
	if err != nil {
		return "", fmt.Errorf("corrupt encrypted password: %w", err)
	}
	gcm, err := newGCM()
	if err != nil {
		return "", err
	}
	if len(raw) < gcm.NonceSize() {
		return "", errors.New("corrupt encrypted password")
	}
	plain, err := gcm.Open(nil, raw[:gcm.NonceSize()], raw[gcm.NonceSize():], nil)
	if err != nil {
		return "", errors.New("can't decrypt the stored password on this machine")
	}
	return string(plain), nil
}

// secretFields calls fn on every stored password: the shared SMTP and inbox
// ones and each profile's mail override.
func (c *Config) secretFields(fn func(*string) error) error {
	ptrs := []*string{&c.Email.SMTP.Password, &c.Inbox.Password}
	for i := range c.Profiles {
		if m := c.Profiles[i].Mail; m != nil {
			if m.Email != nil {
				ptrs = append(ptrs, &m.Email.SMTP.Password)
			}
			if m.Inbox != nil {
				ptrs = append(ptrs, &m.Inbox.Password)
			}
		}
	}
	for _, p := range ptrs {
		if err := fn(p); err != nil {
			return err
		}
	}
	return nil
}

// cloneForSecrets copies the parts of c that hold passwords, so encrypting
// them for Save never touches the live config other goroutines read.
func (c *Config) cloneForSecrets() *Config {
	out := *c
	out.Profiles = append([]NamedProfile(nil), c.Profiles...)
	for i, p := range out.Profiles {
		if p.Mail == nil {
			continue
		}
		m := *p.Mail
		if m.Email != nil {
			e := *m.Email
			m.Email = &e
		}
		if m.Inbox != nil {
			in := *m.Inbox
			m.Inbox = &in
		}
		out.Profiles[i].Mail = &m
	}
	return &out
}
