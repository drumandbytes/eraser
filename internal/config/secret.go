package config

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"os/user"
	"strings"
	"sync"
)

// Passwords are stored in config.yaml as "enc:v1:<base64(nonce||ciphertext)>",
// AES-256-GCM under a key derived from this machine's ID and the current
// user. A config copied to another machine (backup, dotfile sync, a pasted
// file) can't be decrypted there; someone logged in as you on this machine
// still can, as they could by running Eraser. That is the trade for sending
// unattended without a passphrase or keychain.
const secretPrefix = "enc:v1:"

// With a passphrase set the stored form is "enc:v2:<base64(salt||nonce||ciphertext)>"
// and the key also needs the passphrase (PBKDF2-SHA256, so guessing is slow).
// The passphrase is never stored; it is asked for when a password is first
// needed and kept in memory for the life of the process.
const (
	secretPrefixV2 = "enc:v2:"
	pbkdf2Rounds   = 600_000
	saltSize       = 16
)

// machineID returns a stable per-machine identifier. Replaceable in tests.
var machineID = readMachineID

// machineSecret is the machine-and-user binding both key schemes start from.
func machineSecret() ([]byte, error) {
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
	return []byte(id + "\x00" + who), nil
}

func secretKey() ([]byte, error) {
	ikm, err := machineSecret()
	if err != nil {
		return nil, err
	}
	return hkdf.Key(sha256.New, ikm, []byte("eraser/config-secret/v1"), "aes-256-gcm", 32)
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

func isEncrypted(s string) bool {
	return strings.HasPrefix(s, secretPrefix) || strings.HasPrefix(s, secretPrefixV2)
}

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
	if !strings.HasPrefix(s, secretPrefix) {
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

// ErrLocked means a passphrase-protected password was needed but nobody could
// be asked (an unattended run).
var ErrLocked = errors.New("the stored passwords are protected by a passphrase and this run can't ask for it; run `eraser auto loop` or `eraser serve` and enter it there, or remove it with `eraser passphrase clear`")

var (
	unlockMu sync.Mutex
	unlocked string
	// PassphraseProvider asks the person for the passphrase; nil, or an error,
	// means it can't be asked.
	PassphraseProvider func() (string, error)
)

// SetPassphrase remembers pass for this process.
func SetPassphrase(pass string) {
	unlockMu.Lock()
	defer unlockMu.Unlock()
	unlocked = pass
}

func passphrase() (string, error) {
	unlockMu.Lock()
	defer unlockMu.Unlock()
	if unlocked != "" {
		return unlocked, nil
	}
	if PassphraseProvider == nil {
		return "", ErrLocked
	}
	p, err := PassphraseProvider()
	if err != nil {
		return "", fmt.Errorf("%w (%v)", ErrLocked, err)
	}
	unlocked = p
	return p, nil
}

func v2Key(pass string, salt []byte) ([]byte, error) {
	ikm, err := machineSecret()
	if err != nil {
		return nil, err
	}
	return pbkdf2.Key(sha256.New, pass+"\x00"+string(ikm), salt, pbkdf2Rounds, 32)
}

func encryptV2(plain, pass string) (string, error) {
	salt := make([]byte, saltSize)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key, err := v2Key(pass, salt)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	out := append(salt, nonce...)
	return secretPrefixV2 + base64.StdEncoding.EncodeToString(gcm.Seal(out, nonce, []byte(plain), nil)), nil
}

var errWrongPassphrase = errors.New("wrong passphrase (or the config was moved to another machine or user)")

func decryptV2(s, pass string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(s, secretPrefixV2))
	if err != nil || len(raw) < saltSize {
		return "", errors.New("corrupt encrypted password")
	}
	key, err := v2Key(pass, raw[:saltSize])
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	body := raw[saltSize:]
	if len(body) < gcm.NonceSize() {
		return "", errors.New("corrupt encrypted password")
	}
	plain, err := gcm.Open(nil, body[:gcm.NonceSize()], body[gcm.NonceSize():], nil)
	if err != nil {
		return "", errWrongPassphrase
	}
	return string(plain), nil
}

// Reveal returns the usable password for a stored value. Plain and
// machine-bound values are already decrypted by Load; a passphrase-protected
// one is decrypted here, asking for the passphrase the first time.
func Reveal(stored string) (string, error) {
	if !strings.HasPrefix(stored, secretPrefixV2) {
		return stored, nil
	}
	pass, err := passphrase()
	if err != nil {
		return "", err
	}
	plain, err := decryptV2(stored, pass)
	if errors.Is(err, errWrongPassphrase) {
		SetPassphrase("") // ask again next time
	}
	return plain, err
}

// Unlock asks for the passphrase now (up to three tries) and checks it against
// every stored password, so a long-running `serve` or `auto loop` fails at
// startup instead of on the first send. No-op without a passphrase.
func (c *Config) Unlock() error {
	if !c.PassphraseProtected {
		return nil
	}
	var last error
	for range 3 {
		last = c.secretFields(func(p *string) error {
			_, err := Reveal(*p)
			return err
		})
		if last == nil || !errors.Is(last, errWrongPassphrase) {
			return last
		}
		fmt.Fprintln(os.Stderr, "Wrong passphrase.")
	}
	return last
}

// SetPassphraseProtection switches the stored passwords to pass (an empty pass
// goes back to machine-bound encryption). Decrypting the current ones may ask
// for the existing passphrase. Call Save afterwards.
func (c *Config) SetPassphraseProtection(pass string) error {
	if err := c.secretFields(func(p *string) error {
		plain, err := Reveal(*p)
		*p = plain
		return err
	}); err != nil {
		return err
	}
	c.PassphraseProtected = pass != ""
	SetPassphrase(pass)
	return nil
}

// PassphraseUnlocked reports whether this process holds the passphrase.
func PassphraseUnlocked() bool {
	unlockMu.Lock()
	defer unlockMu.Unlock()
	return unlocked != ""
}

// CheckPassphrase verifies pass against every stored password without
// touching the passphrase this process holds, so a typo in a form can't lock
// a running app. Nil when no passphrase is set.
func (c *Config) CheckPassphrase(pass string) error {
	if !c.PassphraseProtected {
		return nil
	}
	return c.secretFields(func(p *string) error {
		if !strings.HasPrefix(*p, secretPrefixV2) {
			return nil
		}
		_, err := decryptV2(*p, pass)
		return err
	})
}

// Clone copies the parts of the config that hold passwords, for callers that
// change them without touching the live config others are reading.
func (c *Config) Clone() *Config { return c.cloneForSecrets() }
