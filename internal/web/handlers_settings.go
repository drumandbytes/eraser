package web

import (
	"fmt"
	"net/http"

	"github.com/eraser-privacy/eraser/internal/broker"
	"github.com/eraser-privacy/eraser/internal/config"
	emaTemplate "github.com/eraser-privacy/eraser/internal/template"
)

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	s.renderSettings(w, r, nil)
}

// templateChoice is one entry in the Selected Template dropdown.
type templateChoice struct {
	Name        string
	Description string
	Selected    bool
}

// templateDescriptions gives each template a one-line summary in the
// dropdown, so the choice can be made without opening the preview - the
// names alone don't say which law a template invokes or whether it asks for
// data, deletes it, or both.
var templateDescriptions = map[string]string{
	"gdpr":        "EU GDPR erasure (Art. 17)",
	"ccpa":        "California CCPA deletion",
	"generic":     "Several privacy laws, works anywhere",
	"uk-access":   "UK GDPR access (Art. 15) - asks what they hold",
	"uk-erasure":  "UK GDPR erasure (Art. 17)",
	"uk-combined": "UK GDPR access + erasure, access answered first",
}

// templatePreview is what the preview partial renders. Error is set instead
// of Subject/Body when the template can't be rendered.
type templatePreview struct {
	Subject string
	Body    string
	Error   string
}

// previewFor renders templateName with the active profile and a sample
// broker, so the preview shows the letter as it will actually be sent -
// name, address and contact details filled in - rather than raw
// placeholders.
func (s *Server) previewFor(r *http.Request, templateName string) templatePreview {
	if s.tmplEngine == nil {
		return templatePreview{Error: "Template engine unavailable."}
	}
	if !s.tmplEngine.IsKnownTemplate(templateName) {
		return templatePreview{Error: fmt.Sprintf("Unknown template %q.", templateName)}
	}

	sample := broker.Broker{
		Name:    "Example Broker",
		Email:   "privacy@example-broker.com",
		Website: "https://example-broker.com",
	}
	rendered, err := s.tmplEngine.Render(templateName, s.activeProfile(r).Profile, sample)
	if err != nil {
		return templatePreview{Error: "Could not render this template: " + err.Error()}
	}
	return templatePreview{Subject: rendered.Subject, Body: rendered.Body}
}

// renderSettings draws the settings page, merging in any extra keys (status
// messages from a save) supplied by the caller.
func (s *Server) renderSettings(w http.ResponseWriter, r *http.Request, extra map[string]interface{}) {
	cfg := s.getConfig()

	selected := ""
	if cfg != nil {
		selected = cfg.Options.Template
	}
	if selected == "" {
		selected = "gdpr"
	}

	choices := make([]templateChoice, 0, len(emaTemplate.TemplateNames()))
	for _, name := range emaTemplate.TemplateNames() {
		choices = append(choices, templateChoice{
			Name:        name,
			Description: templateDescriptions[name],
			Selected:    name == selected,
		})
	}

	data := map[string]interface{}{
		"Title":     "Settings",
		"Config":    cfg,
		"Templates": choices,
		"Preview":   s.previewFor(r, selected),
	}
	for k, v := range extra {
		data[k] = v
	}
	s.renderWithCSRF(w, r, "settings.html", data)
}

// handleSettingsTemplate saves the template used for every send. The CLI and
// the web UI both read options.template, so this is the same setting `eraser
// send` uses when no --template flag overrides it.
func (s *Server) handleSettingsTemplate(w http.ResponseWriter, r *http.Request) {
	limitFormBody(w, r)
	if err := r.ParseForm(); err != nil {
		s.renderSettings(w, r, map[string]interface{}{
			"TemplateMessage": "Failed to parse form", "TemplateSuccess": false,
		})
		return
	}

	name := r.FormValue("template")
	// Validate against the engine rather than trusting the posted value: an
	// unknown name would be written to config.yaml and then fail at send
	// time, once per broker, with no obvious cause.
	if s.tmplEngine == nil || !s.tmplEngine.IsKnownTemplate(name) {
		s.renderSettings(w, r, map[string]interface{}{
			"TemplateMessage": fmt.Sprintf("Unknown template %q.", name), "TemplateSuccess": false,
		})
		return
	}

	// Load-copy-mutate-store, matching handleSettingsInbox: a concurrent
	// reader (another handler, or a running send job) may be holding the
	// pointer returned by getConfig.
	cfg := s.getConfig()
	if cfg == nil {
		cfg = &config.Config{}
	}
	newCfg := *cfg
	newCfg.Options.Template = name

	if err := config.Save(s.configPath, &newCfg); err != nil {
		s.renderSettings(w, r, map[string]interface{}{
			"TemplateMessage": "Failed to save configuration: " + err.Error(), "TemplateSuccess": false,
		})
		return
	}
	s.config.Store(&newCfg)

	s.renderSettings(w, r, map[string]interface{}{
		"TemplateMessage": fmt.Sprintf("Saved. Removal requests will use %q.", name),
		"TemplateSuccess": true,
	})
}

// handleAPITemplatePreview backs the dropdown's live preview. It only
// renders - selecting a template in the dropdown does not change what gets
// sent until the form is saved.
func (s *Server) handleAPITemplatePreview(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("template")
	if name == "" {
		if cfg := s.getConfig(); cfg != nil {
			name = cfg.Options.Template
		}
	}
	s.renderPartial(w, "partials/template-preview.html", s.previewFor(r, name))
}

func (s *Server) handleSettingsInbox(w http.ResponseWriter, r *http.Request) {
	limitFormBody(w, r)
	if err := r.ParseForm(); err != nil {
		s.renderSettingsWithMessage(w, r, "Failed to parse form", false)
		return
	}

	email := r.FormValue("inbox_email")
	password := r.FormValue("inbox_password")

	// Update config with inbox settings. Load-copy-mutate-store rather than
	// mutating the struct returned by getConfig() in place - a concurrent
	// reader (another handler, or a background send-job goroutine) may be
	// holding that exact pointer.
	cfg := s.getConfig()
	if cfg == nil {
		cfg = &config.Config{}
	}
	newCfg := *cfg

	// An empty password means "keep the stored one", not "clear it". The
	// form no longer renders the saved password back into the page (it would
	// be readable by anything that can reach this origin), so an unchanged
	// field arrives blank on every save - without this, editing just the
	// email address would wipe the working IMAP credential.
	if password == "" {
		password = newCfg.Inbox.Password
	}

	// Validate required fields. Checked after the substitution above, so a
	// genuinely new setup with no password anywhere is still rejected.
	if email == "" || password == "" {
		s.renderSettingsWithMessage(w, r, "Email and password are required", false)
		return
	}

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
	// Routed through renderSettings so an inbox save still renders the
	// template selector and its preview - building the data map by hand here
	// would leave .Templates empty and silently drop the dropdown.
	s.renderSettings(w, r, map[string]interface{}{
		"InboxMessage": message,
		"InboxSuccess": success,
	})
}
