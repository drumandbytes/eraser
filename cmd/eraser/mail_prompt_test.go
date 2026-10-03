package main

import (
	"bufio"
	"os"
	"strings"
	"testing"

	"github.com/drumandbytes/eraser/internal/config"
)

func scripted(t *testing.T, lines ...string) *bufio.Reader {
	t.Helper()
	stdout := os.Stdout
	os.Stdout, _ = os.Open(os.DevNull)
	t.Cleanup(func() { os.Stdout = stdout })
	return bufio.NewReader(strings.NewReader(strings.Join(lines, "\n") + "\n"))
}

// A preset fills the servers; only address and password are asked.
func TestPromptMailAccountPreset(t *testing.T) {
	// 2 = Proton Bridge
	e, inbox := promptMailAccount(scripted(t, "2", "me@proton.me", "bridge-pw"), "", config.EmailConfig{})
	if e.SMTP.Host != "127.0.0.1" || e.SMTP.Port != 1025 || e.From != "me@proton.me" || e.SMTP.Username != "me@proton.me" || e.SMTP.Password != "bridge-pw" {
		t.Fatalf("smtp = %+v", e)
	}
	if inbox == nil || inbox.Server != "127.0.0.1" || inbox.Port != 1143 || inbox.Email != "me@proton.me" {
		t.Fatalf("inbox = %+v", inbox)
	}
}

// SES asks host (region) and a separate login, and has no inbox.
func TestPromptMailAccountSES(t *testing.T) {
	e, inbox := promptMailAccount(scripted(t, "7", "email-smtp.eu-central-1.amazonaws.com", "", "me@example.org", "AKIAX", "secret"), "", config.EmailConfig{})
	if e.SMTP.Host != "email-smtp.eu-central-1.amazonaws.com" || e.SMTP.Port != 465 || e.SMTP.Username != "AKIAX" || e.From != "me@example.org" {
		t.Fatalf("smtp = %+v", e)
	}
	if inbox != nil {
		t.Fatalf("SES must be send-only, got inbox %+v", inbox)
	}
}
