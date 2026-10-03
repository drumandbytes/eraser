package config

import (
	"crypto/tls"
	"net"
	"strings"
)

// Provider is a mail-account preset. It only prefills setup forms and fills
// a missing inbox server/port at load; the stored config is plain SMTP/IMAP.
type Provider struct {
	ID       string
	Name     string
	SMTPHost string
	SMTPPort int
	IMAPHost string // "" = send-only (no mailbox, e.g. SES)
	IMAPPort int
	HelpURL  string // where to create the app/bridge password
	Note     string
}

// Providers is the preset list shown in setup, in display order. Adding a
// provider = one entry here (see CONTRIBUTING.md). TLS mode needs no field:
// it follows from the port, see ImplicitTLS.
var Providers = []Provider{
	{ID: "gmail", Name: "Gmail", SMTPHost: "smtp.gmail.com", SMTPPort: 465, IMAPHost: "imap.gmail.com", IMAPPort: 993,
		HelpURL: "https://myaccount.google.com/apppasswords", Note: "Needs 2-Step Verification and a 16-character app password."},
	// Unverified: no maintainer has a Proton account to test Bridge against.
	// Drop the label once a user confirms it works (mail-provider issue).
	{ID: "proton", Name: "Proton Mail (Bridge, unverified)", SMTPHost: "127.0.0.1", SMTPPort: 1025, IMAPHost: "127.0.0.1", IMAPPort: 1143,
		HelpURL: "https://proton.me/mail/bridge", Note: "Requires Proton Mail Bridge running on this machine (paid plan). Use the password Bridge shows, not your Proton password. Not yet tested by the maintainers - if it works (or doesn't) for you, please say so: https://github.com/drumandbytes/eraser/issues/new?template=mail_provider.yml"},
	{ID: "fastmail", Name: "Fastmail", SMTPHost: "smtp.fastmail.com", SMTPPort: 465, IMAPHost: "imap.fastmail.com", IMAPPort: 993,
		HelpURL: "https://www.fastmail.help/", Note: "Create an app password with IMAP + SMTP access."},
	{ID: "mailbox-org", Name: "mailbox.org", SMTPHost: "smtp.mailbox.org", SMTPPort: 465, IMAPHost: "imap.mailbox.org", IMAPPort: 993,
		HelpURL: "https://kb.mailbox.org/", Note: "An application password is recommended."},
	{ID: "posteo", Name: "Posteo", SMTPHost: "posteo.de", SMTPPort: 465, IMAPHost: "posteo.de", IMAPPort: 993,
		HelpURL: "https://posteo.de/en/help", Note: "Uses your Posteo address and password."},
	{ID: "icloud", Name: "iCloud Mail", SMTPHost: "smtp.mail.me.com", SMTPPort: 587, IMAPHost: "imap.mail.me.com", IMAPPort: 993,
		HelpURL: "https://support.apple.com/en-us/102654", Note: "Needs an app-specific password."},
	{ID: "ses", Name: "Amazon SES (send only)", SMTPHost: "email-smtp.eu-west-1.amazonaws.com", SMTPPort: 465,
		HelpURL: "https://docs.aws.amazon.com/ses/latest/dg/smtp-credentials.html", Note: "Use SES SMTP credentials (username is not your address) and change the region in the host if needed. The From address must be verified in SES. SES has no inbox: monitor replies through a separate account."},
	{ID: "custom", Name: "Other (custom SMTP/IMAP)", Note: "Any provider with SMTP and IMAP access. Port 465/993 use TLS directly; other ports use STARTTLS."},
}

func providerIDsWithIMAP() []string {
	var ids []string
	for _, p := range Providers {
		if p.IMAPHost != "" {
			ids = append(ids, p.ID)
		}
	}
	return ids
}

// ProviderByID returns the preset with that id, or false.
func ProviderByID(id string) (Provider, bool) {
	for _, p := range Providers {
		if p.ID == strings.ToLower(id) {
			return p, true
		}
	}
	return Provider{}, false
}

// ProviderIDForHosts guesses the preset of a saved account by host (IMAP only
// when there's no SMTP host). Nothing saved = gmail, the historical default;
// an unknown host = custom.
func ProviderIDForHosts(smtpHost, imapHost string) string {
	if smtpHost == "" && imapHost == "" {
		return "gmail"
	}
	for _, p := range Providers {
		if (smtpHost != "" && strings.EqualFold(p.SMTPHost, smtpHost)) ||
			(smtpHost == "" && p.IMAPHost != "" && strings.EqualFold(p.IMAPHost, imapHost)) {
			return p.ID
		}
	}
	if strings.HasSuffix(strings.ToLower(smtpHost), ".amazonaws.com") {
		return "ses" // any region
	}
	return "custom"
}

// ProviderName labels an SMTP host for display: the preset name, else the host.
func ProviderName(smtpHost string) string {
	if p, ok := ProviderByID(ProviderIDForHosts(smtpHost, "")); ok && p.ID != "custom" && smtpHost != "" {
		return p.Name
	}
	return smtpHost
}

// ImplicitTLS reports whether port speaks TLS from the first byte (SMTPS 465,
// IMAPS 993). Every other port must upgrade with STARTTLS.
func ImplicitTLS(port int) bool {
	return port == 465 || port == 993
}

// TLSFor is the TLS config for a mail server. Loopback hosts skip certificate
// verification: Proton Mail Bridge serves a self-signed cert on 127.0.0.1.
// ponytail: blanket skip on loopback; pin the exported Bridge cert if a local
// MITM ever matters.
func TLSFor(host string) *tls.Config {
	return &tls.Config{
		ServerName:         host,
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: isLoopback(host), //nolint:gosec // loopback only, see above
	}
}

func isLoopback(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
