package web

import (
	"net/http"

	"github.com/drumandbytes/eraser/internal/config"
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

	// start from the existing inbox so a hand-configured provider keeps its
	// server/port/archive; this form only sets email and password
	inbox := newCfg.Inbox
	inbox.Enabled = true
	inbox.Email = email
	inbox.Password = password
	// Set Gmail server/port explicitly: this goes straight into the live
	// config, and config.Load's defaults only apply at startup (else "dial tcp :0").
	if inbox.Provider == "" || inbox.Provider == "gmail" {
		inbox.Provider = "gmail"
		inbox.Server = "imap.gmail.com"
		inbox.Port = 993
	}
	if inbox.Folder == "" {
		inbox.Folder = "INBOX"
	}
	if inbox.ArchiveFolder == "" {
		inbox.ArchiveFolder = "Eraser"
	}
	newCfg.Inbox = inbox

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
