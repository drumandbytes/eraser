package web

import (
	"net/http"

	"github.com/eraser-privacy/eraser/internal/config"
)

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	data := map[string]interface{}{
		"Title":  "Settings",
		"Config": s.getConfig(),
	}
	s.renderWithCSRF(w, r, "settings.html", data)
}

func (s *Server) handleSettingsInbox(w http.ResponseWriter, r *http.Request) {
	limitFormBody(w, r)
	if err := r.ParseForm(); err != nil {
		s.renderSettingsWithMessage(w, r, "Failed to parse form", false)
		return
	}

	email := r.FormValue("inbox_email")
	password := r.FormValue("inbox_password")

	// Validate required fields
	if email == "" || password == "" {
		s.renderSettingsWithMessage(w, r, "Email and password are required", false)
		return
	}

	// Update config with inbox settings. Load-copy-mutate-store rather than
	// mutating the struct returned by getConfig() in place - a concurrent
	// reader (another handler, or a background send-job goroutine) may be
	// holding that exact pointer.
	cfg := s.getConfig()
	if cfg == nil {
		cfg = &config.Config{}
	}
	newCfg := *cfg
	newCfg.Inbox = config.InboxConfig{
		Enabled:  true,
		Provider: "gmail",
		// This form is Gmail-only (see settings.html) and only collects
		// email/password - the IMAP server/port must be set explicitly here
		// rather than left for config.Load's default-filling to apply,
		// since this struct is also stored directly into the live,
		// already-loaded s.config via s.config.Store below. Leaving these
		// zero-valued produced a "dial tcp :0" connect error on the next
		// inbox scan until the process was restarted (config.Load only
		// fills Gmail defaults when Server=="" during its own load, which
		// doesn't run again until the server restarts).
		Server:        "imap.gmail.com",
		Port:          993,
		Email:         email,
		Password:      password,
		Folder:        "INBOX",
		ArchiveFolder: "Eraser",
	}

	// Save config
	if err := config.Save(s.configPath, &newCfg); err != nil {
		s.renderSettingsWithMessage(w, r, "Failed to save configuration: "+err.Error(), false)
		return
	}

	s.config.Store(&newCfg)

	s.renderSettingsWithMessage(w, r, "Inbox monitoring enabled successfully!", true)
}

func (s *Server) renderSettingsWithMessage(w http.ResponseWriter, r *http.Request, message string, success bool) {
	data := map[string]interface{}{
		"Title":        "Settings",
		"Config":       s.getConfig(),
		"InboxMessage": message,
		"InboxSuccess": success,
	}
	s.renderWithCSRF(w, r, "settings.html", data)
}
