package email

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"strings"

	"github.com/drumandbytes/eraser/internal/config"
)

type SMTPSender struct {
	config config.SMTPConfig
	from   string
}

func NewSMTPSender(cfg config.SMTPConfig, from string) *SMTPSender {
	return &SMTPSender{config: cfg, from: from}
}

func (s *SMTPSender) Send(ctx context.Context, msg Message) Result {
	if err := validateMessage(msg); err != nil {
		return Result{Success: false, Error: err}
	}
	// Reject headers with CRLF to prevent injection
	if strings.ContainsAny(msg.Subject, "\r\n") {
		return Result{Success: false, Error: fmt.Errorf("subject contains invalid characters")}
	}

	addr := fmt.Sprintf("%s:%d", s.config.Host, s.config.Port)

	var message strings.Builder
	fmt.Fprintf(&message, "From: %s\r\n", msg.From)
	fmt.Fprintf(&message, "To: %s\r\n", msg.To)
	fmt.Fprintf(&message, "Subject: %s\r\n", msg.Subject)
	message.WriteString("MIME-Version: 1.0\r\n")
	message.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
	message.WriteString("\r\n")
	message.WriteString(msg.Body)

	var auth smtp.Auth
	if s.config.UseTLS {
		auth = smtp.PlainAuth("", s.config.Username, s.config.Password, s.config.Host)
	} else if s.config.Username != "" {
		return Result{Success: false, Error: fmt.Errorf("SMTP auth requires TLS")}
	}

	var err error
	if s.config.UseTLS {
		err = s.send(ctx, addr, auth, msg.From, msg.To, []byte(message.String()), true)
	} else {
		err = s.send(ctx, addr, nil, msg.From, msg.To, []byte(message.String()), false)
	}
	if err != nil {
		return Result{Success: false, Error: sanitizeSMTPError(err)}
	}

	return Result{
		Success:   true,
		MessageID: fmt.Sprintf("smtp-%s-%d", msg.To, ctx.Value(SequenceKey)),
	}
}

func sanitizeSMTPError(err error) error {
	s := strings.ToLower(err.Error())
	if strings.Contains(s, "auth") {
		return fmt.Errorf("SMTP authentication failed")
	}
	if strings.Contains(s, "certificate") {
		return fmt.Errorf("TLS certificate error")
	}
	return fmt.Errorf("SMTP error: check your configuration")
}

// send dials addr, optionally wraps the connection in TLS, and runs the SMTP
// transaction - all under ctx. net/smtp has no context support of its own
// (smtp.SendMail included, which is why this doesn't just call it), so a
// server that accepts the connection and then never answers would otherwise
// hang the caller forever: the 30s timeout callers set on ctx, and a
// cancelled job's Cancel button, would both be silently ignored. Closing the
// connection when ctx is done is what actually makes those work - net/smtp's
// blocking Read/Write calls return an error the moment the underlying conn
// closes.
func (s *SMTPSender) send(ctx context.Context, addr string, auth smtp.Auth, from, to string, msg []byte, useTLS bool) error {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("connection failed: %w", err)
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}

	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close() // unblocks any in-flight Read/Write below
		case <-done:
		}
	}()

	smtpConn := conn
	if useTLS {
		tlsConn := tls.Client(conn, &tls.Config{
			ServerName: s.config.Host,
			MinVersion: tls.VersionTLS12,
		})
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			_ = conn.Close()
			return fmt.Errorf("TLS handshake failed: %w", err)
		}
		smtpConn = tlsConn
	}

	client, err := smtp.NewClient(smtpConn, s.config.Host)
	if err != nil {
		_ = smtpConn.Close()
		return fmt.Errorf("SMTP client creation failed: %w", err)
	}
	defer func() { _ = client.Close() }()

	if auth != nil {
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}
	}
	if err := client.Mail(from); err != nil {
		return fmt.Errorf("sender rejected: %w", err)
	}
	if err := client.Rcpt(to); err != nil {
		return fmt.Errorf("recipient rejected: %w", err)
	}

	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("data command failed: %w", err)
	}
	if _, err = w.Write(msg); err != nil {
		return fmt.Errorf("message write failed: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("message finalization failed: %w", err)
	}
	return client.Quit()
}
