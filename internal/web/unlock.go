package web

import (
	"net/http"
	"strings"

	"github.com/drumandbytes/eraser/internal/config"
)

const minPassphraseLen = 8

// verifyPassphrase checks pass against the passwords as stored on disk, not the
// in-memory config, which may hold freshly typed plaintext values.
func (s *Server) verifyPassphrase(pass string) error {
	stored, err := config.Load(s.configPath)
	if err != nil {
		return err
	}
	return stored.CheckPassphrase(pass)
}

func (s *Server) locked() bool {
	cfg := s.getConfig()
	return cfg != nil && cfg.PassphraseProtected && !config.PassphraseUnlocked()
}

// requireUnlock keeps everything but the unlock page behind the passphrase when
// one is set and this process doesn't have it yet (serve started without a
// terminal to ask on).
func (s *Server) requireUnlock(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.locked() || r.URL.Path == "/unlock" || strings.HasPrefix(r.URL.Path, "/static/") {
			next.ServeHTTP(w, r)
			return
		}
		if r.Method == http.MethodGet && r.Header.Get("HX-Request") == "" {
			http.Redirect(w, r, "/unlock", http.StatusSeeOther)
			return
		}
		http.Error(w, "Locked: enter your passphrase at /unlock", http.StatusLocked)
	})
}

func (s *Server) renderUnlock(w http.ResponseWriter, r *http.Request, errMsg string) {
	s.renderWithCSRF(w, r, "unlock.html", map[string]interface{}{"Title": "Unlock", "Error": errMsg})
}

func (s *Server) handleUnlock(w http.ResponseWriter, r *http.Request) {
	if !s.locked() {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	if r.Method == http.MethodGet {
		s.renderUnlock(w, r, "")
		return
	}
	limitFormBody(w, r)
	if !s.rateLimiter.Allow("unlock") {
		w.WriteHeader(http.StatusTooManyRequests)
		s.renderUnlock(w, r, "Too many attempts. Wait a moment and try again.")
		return
	}
	if err := r.ParseForm(); err != nil {
		s.renderUnlock(w, r, "Failed to parse form")
		return
	}
	pass := r.FormValue("passphrase")
	if err := s.verifyPassphrase(pass); err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		s.renderUnlock(w, r, "Wrong passphrase.")
		return
	}
	config.SetPassphrase(pass)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// passphraseView is what the Settings card needs.
type passphraseView struct {
	Protected   bool
	SchedulerOn bool
	MinLength   int
}

func (s *Server) passphraseView(cfg *config.Config) passphraseView {
	return passphraseView{
		Protected:   cfg != nil && cfg.PassphraseProtected,
		SchedulerOn: s.osInstalled(),
		MinLength:   minPassphraseLen,
	}
}

func (s *Server) handleSettingsPassphrase(w http.ResponseWriter, r *http.Request) {
	limitFormBody(w, r)
	fail := func(msg string) {
		s.renderSettings(w, r, map[string]interface{}{"PassphraseMessage": msg, "PassphraseSuccess": false})
	}
	if err := r.ParseForm(); err != nil {
		fail("Failed to parse form")
		return
	}
	cfg := s.getConfig()
	if cfg == nil {
		fail("No configuration found - run setup first")
		return
	}
	if !s.rateLimiter.Allow("passphrase") {
		w.WriteHeader(http.StatusTooManyRequests)
		fail("Too many attempts. Wait a moment and try again.")
		return
	}
	if cfg.PassphraseProtected {
		if err := s.verifyPassphrase(r.FormValue("current")); err != nil {
			fail("The current passphrase is wrong")
			return
		}
	}

	next := ""
	if r.FormValue("action") != "clear" {
		next = r.FormValue("new")
		if len(next) < minPassphraseLen {
			fail("Use at least 8 characters")
			return
		}
		if next != r.FormValue("confirm") {
			fail("The passphrases don't match")
			return
		}
	} else if !cfg.PassphraseProtected {
		fail("No passphrase is set")
		return
	}

	updated := cfg.Clone()
	if err := updated.SetPassphraseProtection(next); err != nil {
		fail("Couldn't change the passphrase: " + err.Error())
		return
	}
	if err := s.saveConfig(updated); err != nil {
		fail("Failed to save configuration")
		return
	}
	// Hold what is on disk (opaque ciphertext) rather than the plaintext just typed.
	if stored, err := config.Load(s.configPath); err == nil {
		updated = stored
	}
	s.config.Store(updated)

	msg := "Passphrase removed. Mail passwords are encrypted for this machine and user only."
	if next != "" {
		msg = "Passphrase set. Your mail passwords can't be decrypted without it."
		if s.osInstalled() {
			msg += " The OS schedule can no longer send: remove it under Automation and use in-app automation, or run eraser auto."
		}
	}
	s.renderSettings(w, r, map[string]interface{}{"PassphraseMessage": msg, "PassphraseSuccess": true})
}
