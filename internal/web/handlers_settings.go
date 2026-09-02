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

	// Start from the existing inbox config (not a blank struct) so a
	// provider configured by hand in config.yaml (e.g. "outlook") keeps its
	// own Server/Port/AutoArchive when the user submits this form just to
	// change their email/password - this form only collects those two
	// fields (see settings.html), it has no provider selector.
	inbox := newCfg.Inbox
	inbox.Enabled = true
	inbox.Email = email
	inbox.Password = password
	// Only fill in Gmail defaults for a brand-new setup or an
	// already-Gmail config - the IMAP server/port must be set explicitly
	// here rather than left for config.Load's default-filling to apply,
	// since this struct is also stored directly into the live,
	// already-loaded s.config via s.config.Store below. Leaving these
	// zero-valued produced a "dial tcp :0" connect error on the next
	// inbox scan until the process was restarted (config.Load only fills
	// Gmail defaults when Server=="" during its own load, which doesn't
	// run again until the server restarts).
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
