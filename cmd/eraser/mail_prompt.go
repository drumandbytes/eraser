package main

import (
	"bufio"
	"cmp"
	"fmt"
	"strconv"

	"github.com/drumandbytes/eraser/internal/config"
)

// promptMailAccount asks for a provider preset, then only what the preset
// doesn't fill. existing (may be zero) supplies the defaults. inbox is nil for
// send-only providers (SES) or when a custom provider has no IMAP server.
func promptMailAccount(reader *bufio.Reader, defaultAddr string, existing config.EmailConfig) (config.EmailConfig, *config.InboxConfig) {
	current := config.ProviderIDForHosts(existing.SMTP.Host, "")
	def := "1"
	fmt.Println("  Email provider:")
	for i, p := range config.Providers {
		fmt.Printf("    %d. %s\n", i+1, p.Name)
		if p.ID == current {
			def = strconv.Itoa(i + 1)
		}
	}
	p := config.Providers[0]
	if n, err := strconv.Atoi(promptWithDefault(reader, "  Choose", def)); err == nil && n >= 1 && n <= len(config.Providers) {
		p = config.Providers[n-1]
	}
	if p.Note != "" {
		fmt.Println("  " + p.Note)
	}
	if p.HelpURL != "" {
		fmt.Println("  Setup guide: " + p.HelpURL)
	}

	smtpHost, smtpPort := p.SMTPHost, p.SMTPPort
	imapHost, imapPort := p.IMAPHost, p.IMAPPort
	if p.ID == current && existing.SMTP.Host != "" {
		smtpHost, smtpPort = existing.SMTP.Host, existing.SMTP.Port
	}
	if p.ID == "custom" || p.ID == "ses" { // custom has no hosts; SES host carries the region
		fmt.Println("  Ports 465/993 use TLS directly; any other port uses STARTTLS.")
		smtpHost = promptWithDefault(reader, "  SMTP server", smtpHost)
		smtpPort = promptPort(reader, "  SMTP port", smtpPort, 465)
	}
	if p.ID == "custom" {
		imapHost = promptWithDefault(reader, "  IMAP server for reply monitoring (blank = none)", imapHost)
		if imapHost != "" {
			imapPort = promptPort(reader, "  IMAP port", imapPort, 993)
		}
	}

	addr := promptWithDefault(reader, "  Email address", cmp.Or(existing.From, defaultAddr))
	userDefault := addr
	if existing.SMTP.Username != "" && p.ID == current {
		userDefault = existing.SMTP.Username
	}
	user := userDefault
	if p.ID == "custom" || p.ID == "ses" {
		user = promptWithDefault(reader, "  Login username", userDefault)
	}
	password := promptSecretWithDefault(reader, "  App password", existing.SMTP.Password)

	e := config.EmailConfig{
		From: addr,
		SMTP: config.SMTPConfig{Host: smtpHost, Port: smtpPort, Username: user, Password: password},
	}
	if imapHost == "" {
		return e, nil
	}
	provider := p.ID
	if provider == "custom" {
		provider = ""
	}
	inbox := &config.InboxConfig{Enabled: true, Provider: provider, Server: imapHost, Port: imapPort, Email: user, Password: password}
	config.ApplyInboxDefaults(inbox)
	return e, inbox
}

func promptPort(reader *bufio.Reader, label string, current, fallback int) int {
	if current == 0 {
		current = fallback
	}
	if n, err := strconv.Atoi(promptWithDefault(reader, label, strconv.Itoa(current))); err == nil && n > 0 {
		return n
	}
	return current
}
