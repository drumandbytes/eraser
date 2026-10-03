package web

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/drumandbytes/eraser/internal/config"
	"github.com/drumandbytes/eraser/internal/email"
)

// mailForm is what partials/mail-account.html posts. The provider preset only
// prefills fields; the posted host/port win, so "custom" needs no special case.
type mailForm struct {
	Provider string
	Address  string
	Username string // login; "" = Address (SES's SMTP user isn't an address)
	Password string
	SMTPHost string
	SMTPPort int
	IMAPHost string
	IMAPPort int
}

func readMailForm(r *http.Request) mailForm {
	f := mailForm{
		Provider: strings.TrimSpace(r.FormValue("mail_provider")),
		Address:  strings.TrimSpace(r.FormValue("mail_address")),
		Username: strings.TrimSpace(r.FormValue("mail_username")),
		Password: strings.TrimSpace(r.FormValue("mail_password")),
		SMTPHost: strings.TrimSpace(r.FormValue("smtp_host")),
		IMAPHost: strings.TrimSpace(r.FormValue("imap_host")),
	}
	f.SMTPPort, _ = strconv.Atoi(strings.TrimSpace(r.FormValue("smtp_port")))
	f.IMAPPort, _ = strconv.Atoi(strings.TrimSpace(r.FormValue("imap_port")))
	if f.Username == "" {
		f.Username = f.Address
	}
	// Without JS the server fields stay empty; fall back to the preset.
	if p, ok := config.ProviderByID(f.Provider); ok {
		if f.SMTPHost == "" {
			f.SMTPHost, f.SMTPPort = p.SMTPHost, p.SMTPPort
		}
		if f.IMAPHost == "" {
			f.IMAPHost, f.IMAPPort = p.IMAPHost, p.IMAPPort
		}
	}
	return f
}

// mailFormFromConfig refills the form from a saved account (either may be zero).
func mailFormFromConfig(e config.EmailConfig, in *config.InboxConfig) mailForm {
	f := mailForm{Address: e.From, Username: e.SMTP.Username, SMTPHost: e.SMTP.Host, SMTPPort: e.SMTP.Port}
	if in != nil {
		inbox := *in
		config.ApplyInboxDefaults(&inbox) // provider-only blocks have no server yet
		f.IMAPHost, f.IMAPPort = inbox.Server, inbox.Port
		if f.Address == "" {
			f.Address, f.Username = in.Email, in.Email
		}
		if _, ok := config.ProviderByID(in.Provider); ok && e.SMTP.Host == "" {
			f.Provider = in.Provider
		}
	}
	return f
}

func (f mailForm) emailConfig() config.EmailConfig {
	return config.EmailConfig{
		From: f.Address,
		SMTP: config.SMTPConfig{
			Host:     f.SMTPHost,
			Port:     f.SMTPPort,
			Username: f.Username,
			Password: f.Password,
		},
	}
}

// inboxConfig is nil for send-only accounts (SES).
func (f mailForm) inboxConfig() *config.InboxConfig {
	if f.IMAPHost == "" {
		return nil
	}
	inbox := &config.InboxConfig{
		Enabled:  true,
		Provider: presetID(f.Provider),
		Server:   f.IMAPHost,
		Port:     f.IMAPPort,
		Email:    f.Username,
		Password: f.Password,
	}
	config.ApplyInboxDefaults(inbox)
	return inbox
}

// mailFormView is the partial's data. Mode is "smtp", "imap" or "both".
type mailFormView struct {
	Mode                string
	Provider            string
	Address             string
	Username            string
	PasswordPlaceholder string
	SMTPHost            string
	SMTPPort            int
	IMAPHost            string
	IMAPPort            int
	Errors              map[string]string
	Providers           []config.Provider
}

func newMailFormView(mode string, f mailForm, errors map[string]string) mailFormView {
	if f.Provider == "" {
		f.Provider = config.ProviderIDForHosts(f.SMTPHost, f.IMAPHost)
	}
	if p, ok := config.ProviderByID(f.Provider); ok {
		if f.SMTPHost == "" {
			f.SMTPHost, f.SMTPPort = p.SMTPHost, p.SMTPPort
		}
		if f.IMAPHost == "" {
			f.IMAPHost, f.IMAPPort = p.IMAPHost, p.IMAPPort
		}
	}
	username := f.Username
	if strings.EqualFold(username, f.Address) {
		username = "" // only show a login that differs from the address
	}
	return mailFormView{
		Mode: mode, Provider: f.Provider, Address: f.Address, Username: username,
		SMTPHost: f.SMTPHost, SMTPPort: f.SMTPPort, IMAPHost: f.IMAPHost, IMAPPort: f.IMAPPort,
		Errors: errors, Providers: config.Providers,
	}
}

// validate checks the fields Mode needs; keys match the partial's inputs.
func (f mailForm) validate(mode string, requirePassword bool) map[string]string {
	errors := make(map[string]string)
	if f.Address == "" {
		errors["mail_address"] = "Email address is required"
	} else if email.ValidateEmail(f.Address) != nil {
		errors["mail_address"] = "Please enter a valid email address"
	}
	if requirePassword && f.Password == "" {
		errors["mail_password"] = "Password is required"
	}
	if mode != "imap" && (f.SMTPHost == "" || f.SMTPPort <= 0) {
		errors["smtp_host"] = "SMTP server and port are required"
	}
	if mode == "imap" && (f.IMAPHost == "" || f.IMAPPort <= 0) {
		errors["imap_host"] = "IMAP server and port are required (this provider has no inbox access)"
	}
	return errors
}

// presetID drops "custom" (and unknown ids) so the saved inbox only names a
// real preset.
func presetID(id string) string {
	if p, ok := config.ProviderByID(id); ok && p.IMAPHost != "" {
		return p.ID
	}
	return ""
}
