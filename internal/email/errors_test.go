package email

import (
	"context"
	"errors"
	"net"
	"strconv"
	"strings"
	"testing"

	"github.com/drumandbytes/eraser/internal/config"
)

func TestNewSender(t *testing.T) {
	for _, p := range []string{"", "smtp"} {
		if s, err := NewSender(config.EmailConfig{Provider: p}); err != nil || s == nil {
			t.Errorf("provider %q: %v", p, err)
		}
	}
	if _, err := NewSender(config.EmailConfig{Provider: "sendgrid"}); err == nil {
		t.Error("unknown provider accepted")
	}
}

// Raw SMTP errors can echo credentials or server internals; the user gets
// a short category instead.
func TestSanitizeSMTPError(t *testing.T) {
	for raw, want := range map[string]string{
		"535 5.7.8 Authentication failed":             "SMTP authentication failed",
		"x509: certificate signed by unknown":         "TLS certificate error",
		"dial tcp 127.0.0.1:1025: connection refused": "could not connect",
		"server does not offer STARTTLS on port 25":   "does not support STARTTLS",
		"451 temporary local problem":                 "check your configuration",
	} {
		if got := sanitizeSMTPError(errors.New(raw)).Error(); !strings.Contains(got, want) {
			t.Errorf("%q -> %q, want %q", raw, got, want)
		}
	}
}

func TestSendRejections(t *testing.T) {
	msg := Message{To: "privacy@acme.example", From: "jane@example.org", Subject: "x", Body: "hi"}

	crlf := msg
	crlf.Subject = "x\r\nBcc: victim@example.com"
	if res := NewSMTPSender(config.SMTPConfig{Host: "127.0.0.1", Port: 1}, msg.From).Send(context.Background(), crlf); res.Success {
		t.Error("CRLF in subject accepted")
	}

	noTLS := false
	plainAuth := NewSMTPSender(config.SMTPConfig{Host: "127.0.0.1", Port: 1, Username: "jane", UseTLS: &noTLS}, msg.From)
	if res := plainAuth.Send(context.Background(), msg); res.Success || !strings.Contains(res.Error.Error(), "requires TLS") {
		t.Errorf("plaintext auth: %+v", res)
	}

	refused := NewSMTPSender(config.SMTPConfig{Host: "127.0.0.1", Port: 1, UseTLS: &noTLS}, msg.From)
	if res := refused.Send(context.Background(), msg); res.Success || !strings.Contains(res.Error.Error(), "could not connect") {
		t.Errorf("refused: %+v", res)
	}
}

// A server that rejects each step surfaces as a failed send, not a hang.
func TestSendServerRejections(t *testing.T) {
	for _, step := range []string{"MAIL", "RCPT", "DATA", "BODY"} {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		go func(step string) {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			defer func() { _ = conn.Close() }()
			buf := make([]byte, 4096)
			_, _ = conn.Write([]byte("220 hi\r\n"))
			for {
				n, err := conn.Read(buf)
				if err != nil {
					return
				}
				line := strings.ToUpper(string(buf[:n]))
				switch {
				case strings.HasPrefix(line, "EHLO"), strings.HasPrefix(line, "HELO"):
					_, _ = conn.Write([]byte("250 hi\r\n"))
				case strings.HasPrefix(line, step):
					_, _ = conn.Write([]byte("554 no\r\n"))
				case strings.HasPrefix(line, "DATA"):
					_, _ = conn.Write([]byte("354 go\r\n"))
				case strings.HasSuffix(line, "\r\n.\r\n") && step == "BODY":
					_, _ = conn.Write([]byte("554 no\r\n"))
				default:
					_, _ = conn.Write([]byte("250 ok\r\n"))
				}
			}
		}(step)
		_, portStr, _ := net.SplitHostPort(ln.Addr().String())
		port, _ := strconv.Atoi(portStr)
		noTLS := false
		s := NewSMTPSender(config.SMTPConfig{Host: "127.0.0.1", Port: port, UseTLS: &noTLS}, "jane@example.org")
		res := s.Send(context.Background(), Message{To: "privacy@acme.example", From: "jane@example.org", Subject: "x", Body: "hi"})
		if res.Success {
			t.Errorf("%s rejected but send succeeded", step)
		}
		_ = ln.Close()
	}
}
