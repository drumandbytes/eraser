package email

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"fmt"
	"net"
	"net/mail"
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

	// Our own Message-ID, so the one recorded in history is the one the
	// broker actually receives (and quotes back in In-Reply-To).
	domain := "localhost"
	if from, err := mail.ParseAddress(msg.From); err == nil {
		domain = from.Address[strings.LastIndex(from.Address, "@")+1:]
	}
	messageID := "<" + rand.Text() + "@" + domain + ">"

	var message strings.Builder
	fmt.Fprintf(&message, "Message-ID: %s\r\n", messageID)
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
		MessageID: messageID,
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
	if strings.Contains(s, "connection refused") {
		return fmt.Errorf("could not connect to the SMTP server (wrong host/port, or a local bridge like Proton Mail Bridge isn't running)")
	}
	if strings.Contains(s, "starttls") {
		return fmt.Errorf("SMTP server does not support STARTTLS on this port")
	}
	return fmt.Errorf("SMTP error: check your configuration")
}

// send runs the SMTP transaction under ctx. net/smtp has no context support,
// so a silent server would hang forever and ignore timeouts and Cancel;
// closing the conn on ctx.Done unblocks its reads and writes.
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

	// 465 is TLS from the first byte; any other port upgrades with STARTTLS.
	implicit := useTLS && config.ImplicitTLS(s.config.Port)
	smtpConn := conn
	if implicit {
		tlsConn := tls.Client(conn, config.TLSFor(s.config.Host))
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

	if useTLS && !implicit {
		// Required, never opportunistic: auth must not go out in plaintext.
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return fmt.Errorf("server does not offer STARTTLS on port %d", s.config.Port)
		}
		if err := client.StartTLS(config.TLSFor(s.config.Host)); err != nil {
			return fmt.Errorf("STARTTLS failed: %w", err)
		}
	}

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
