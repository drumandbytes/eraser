package web

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/drumandbytes/delistd/internal/config"
	"github.com/drumandbytes/delistd/internal/email"
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
	// OAuth (Outlook.com): ClientID is the user's Microsoft app, and a
	// finished sign-in (oauth_id) supplies the refresh token as Password.
	OAuth    bool
	ClientID string
	hasToken bool // a sign-in is already saved (mailFormFromConfig)
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
	if f.Provider == config.OutlookOAuth.ID {
		f.OAuth = true
		f.ClientID = strings.TrimSpace(r.FormValue("oauth_client_id"))
		f.Password = "" // a password never applies here
		if tok, ok := signIns.token(strings.TrimSpace(r.FormValue("oauth_id")), f.ClientID); ok {
			f.Password = tok
		}
		p := config.OutlookOAuth
		if f.SMTPHost == "" {
			f.SMTPHost, f.SMTPPort = p.SMTPHost, p.SMTPPort
		}
		if f.IMAPHost == "" {
			f.IMAPHost, f.IMAPPort = p.IMAPHost, p.IMAPPort
		}
	} else if p, ok := config.ProviderByID(f.Provider); ok {
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
	f.hasToken = e.SMTP.Auth == config.AuthOAuth2 && e.SMTP.Password != ""
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
		if in.Auth == config.AuthOAuth2 && f.ClientID == "" {
			f.OAuth, f.ClientID, f.Provider = true, in.ClientID, config.OutlookOAuth.ID
			f.hasToken = f.hasToken || in.Password != ""
		}
	}
	if e.SMTP.Auth == config.AuthOAuth2 {
		f.OAuth, f.ClientID, f.Provider = true, e.SMTP.ClientID, config.OutlookOAuth.ID
	}
	return f
}

func (f mailForm) emailConfig() config.EmailConfig {
	e := config.EmailConfig{
		From: f.Address,
		SMTP: config.SMTPConfig{
			Host:     f.SMTPHost,
			Port:     f.SMTPPort,
			Username: f.Username,
			Password: f.Password,
		},
	}
	if f.OAuth {
		e.SMTP.Auth, e.SMTP.ClientID = config.AuthOAuth2, f.ClientID
	}
	return e
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
	if f.OAuth {
		inbox.Auth, inbox.ClientID = config.AuthOAuth2, f.ClientID
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
	OAuth               bool   // Outlook.com selected: show the sign-in panel, not a password
	ClientID            string // saved OAuth app id
	SignedIn            bool   // an OAuth sign-in is already stored for this account
	SharedApp           bool   // Delistd's own Microsoft app is available: the guide is only for bringing your own
}

func newMailFormView(mode string, f mailForm, errors map[string]string) mailFormView {
	if f.Provider == "" {
		f.Provider = config.ProviderIDForHosts(f.SMTPHost, f.IMAPHost)
	}
	if f.Provider == config.OutlookOAuth.ID {
		f.OAuth = true
	}
	if p, ok := config.ProviderByID(f.Provider); ok {
		if f.SMTPHost == "" {
			f.SMTPHost, f.SMTPPort = p.SMTPHost, p.SMTPPort
		}
		if f.IMAPHost == "" {
			f.IMAPHost, f.IMAPPort = p.IMAPHost, p.IMAPPort
		}
	} else if f.OAuth {
		p := config.OutlookOAuth
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
		Errors: errors, Providers: providersForForm(),
		OAuth: f.OAuth, ClientID: f.ClientID, SignedIn: f.OAuth && (f.Password != "" || f.hasToken), SharedApp: config.DefaultOAuthClientID != "",
	}
}

// keepStored reuses the saved secret when the form left it blank for the same
// login, and for OAuth only when it is the same kind of sign-in (a saved app
// password is not a refresh token).
func (f *mailForm) keepStored(user, secret, auth, clientID string) {
	if f.Password != "" || !strings.EqualFold(f.Username, user) {
		return
	}
	if (auth == config.AuthOAuth2) != f.OAuth || (f.OAuth && clientID != f.ClientID) {
		return
	}
	f.Password = secret
}

// providersForForm is config.Providers with the Outlook OAuth entry before
// "Other", which stays last.
func providersForForm() []config.Provider {
	n := len(config.Providers)
	out := make([]config.Provider, 0, n+1)
	out = append(out, config.Providers[:n-1]...)
	return append(out, config.OutlookOAuth, config.Providers[n-1])
}

// validate checks the fields Mode needs; keys match the partial's inputs.
func (f mailForm) validate(mode string, requirePassword bool) map[string]string {
	errors := make(map[string]string)
	if f.Address == "" {
		errors["mail_address"] = "Email address is required"
	} else if email.ValidateEmail(f.Address) != nil {
		errors["mail_address"] = "Please enter a valid email address"
	}
	if f.OAuth {
		if config.OAuthClientID(f.ClientID) == "" {
			errors["oauth"] = "Paste your Microsoft app's client ID, then sign in"
		} else if requirePassword && f.Password == "" {
			errors["oauth"] = "Sign in with Microsoft first"
		}
	} else if requirePassword && f.Password == "" {
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
