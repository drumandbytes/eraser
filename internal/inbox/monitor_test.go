package inbox

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"

	"github.com/drumandbytes/eraser/internal/config"
	gomail "github.com/emersion/go-message/mail"
)

// buildMIMEMessage builds a single-part text/plain message with 8bit encoding,
// so the decoded body equals the input.
func buildMIMEMessage(t *testing.T, body string) []byte {
	t.Helper()

	var buf bytes.Buffer
	var h gomail.Header
	h.Set("Content-Type", "text/plain; charset=us-ascii")
	h.Set("Content-Transfer-Encoding", "8bit")

	wc, err := gomail.CreateSingleInlineWriter(&buf, h)
	if err != nil {
		t.Fatalf("CreateSingleInlineWriter: %v", err)
	}
	if _, err := io.WriteString(wc, body); err != nil {
		t.Fatalf("writing body: %v", err)
	}
	if err := wc.Close(); err != nil {
		t.Fatalf("closing writer: %v", err)
	}

	return buf.Bytes()
}

// An oversized MIME part must be capped at maxMIMEPartBytes (inbox content is
// attacker-controlled).
func TestParseMessageCapsOversizedMIMEPart(t *testing.T) {
	const oversizeBy = 5 << 20 // 5MB past the cap
	const size = maxMIMEPartBytes + oversizeBy

	raw := buildMIMEMessage(t, strings.Repeat("x", size))

	section := &imap.BodySectionName{}
	msg := &imap.Message{
		Uid: 42,
		Envelope: &imap.Envelope{
			Subject: "Your data removal request",
			From: []*imap.Address{
				{PersonalName: "Broker Support", MailboxName: "support", HostName: "broker.example.com"},
			},
		},
		Body: map[*imap.BodySectionName]imap.Literal{
			section: bytes.NewReader(raw),
		},
	}

	m := &Monitor{}
	email, err := m.parseMessage(msg, section)
	if err != nil {
		t.Fatalf("parseMessage returned error: %v", err)
	}
	if email == nil {
		t.Fatal("parseMessage returned nil email")
	}

	if len(email.Body) > maxMIMEPartBytes {
		t.Errorf("parsed body length = %d, must not exceed maxMIMEPartBytes (%d)", len(email.Body), maxMIMEPartBytes)
	}
	// With Content-Transfer-Encoding: 8bit the bytes pass through undecoded,
	// so the LimitReader should cap the read at exactly maxMIMEPartBytes
	// given an input larger than the cap.
	if len(email.Body) != maxMIMEPartBytes {
		t.Errorf("parsed body length = %d, want exactly maxMIMEPartBytes (%d)", len(email.Body), maxMIMEPartBytes)
	}
}

// TestParseMessageDoesNotPadSmallBodies is a sanity check that the cap only
// clips oversized parts and doesn't otherwise change parsing behavior for
// normal-sized emails.
func TestParseMessageDoesNotPadSmallBodies(t *testing.T) {
	const body = "Please visit our opt-out page at https://broker.example.com/opt-out"
	raw := buildMIMEMessage(t, body)

	section := &imap.BodySectionName{}
	msg := &imap.Message{
		Uid:      1,
		Envelope: &imap.Envelope{Subject: "test"},
		Body: map[*imap.BodySectionName]imap.Literal{
			section: bytes.NewReader(raw),
		},
	}

	m := &Monitor{}
	email, err := m.parseMessage(msg, section)
	if err != nil {
		t.Fatalf("parseMessage returned error: %v", err)
	}
	if email.Body != body {
		t.Errorf("parsed body = %q, want %q", email.Body, body)
	}
}

// ---------------------------------------------------------------------
// Fake IMAP server helpers for code that needs a real *client.Client.
// ---------------------------------------------------------------------

// fakeIMAPServer runs a scripted conversation against one accepted
// connection. handle is invoked with the accepted net.Conn and a
// bufio.Reader wrapping it; any error it returns is surfaced via t.Errorf
// during test cleanup (after giving the handler a bounded time to finish).
func fakeIMAPServer(t *testing.T, handle func(conn net.Conn, br *bufio.Reader) error) string {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start fake IMAP listener: %v", err)
	}

	connCh := make(chan net.Conn, 1)
	doneCh := make(chan error, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			doneCh <- fmt.Errorf("accept: %w", err)
			return
		}
		connCh <- conn
		doneCh <- handle(conn, bufio.NewReader(conn))
	}()

	t.Cleanup(func() {
		_ = ln.Close()
		select {
		case conn := <-connCh:
			_ = conn.Close()
		default:
		}
		select {
		case err := <-doneCh:
			if err != nil && !isExpectedCloseErr(err) {
				t.Errorf("fake IMAP server: %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Errorf("fake IMAP server: handler did not finish within timeout")
		}
	})

	return ln.Addr().String()
}

// isExpectedCloseErr reports whether err is the expected result of the test
// cleanup closing the connection out from under a handler that was
// deliberately left blocked reading (used by the ctx-cancellation tests).
func isExpectedCloseErr(err error) bool {
	return strings.Contains(err.Error(), "use of closed network connection") ||
		err == io.EOF || strings.Contains(err.Error(), "EOF")
}

// readCommandLine reads one CRLF-terminated line and returns its IMAP tag
// and the rest of the line (the command + arguments).
func readCommandLine(br *bufio.Reader) (tag, rest string, err error) {
	line, err := br.ReadString('\n')
	if err != nil {
		return "", "", err
	}
	line = strings.TrimRight(line, "\r\n")
	parts := strings.SplitN(line, " ", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("malformed command line %q", line)
	}
	return parts[0], parts[1], nil
}

func writeLines(conn net.Conn, lines ...string) error {
	for _, l := range lines {
		if _, err := io.WriteString(conn, l+"\r\n"); err != nil {
			return err
		}
	}
	return nil
}

// TestDeletedUIDsBesides runs against a scripted loopback IMAP server:
// *client.Client is concrete, and UidSearch needs the "selected" state,
// reachable only over the wire (PREAUTH greeting, then SELECT and UID SEARCH).
func TestDeletedUIDsBesides(t *testing.T) {
	addr := fakeIMAPServer(t, func(conn net.Conn, br *bufio.Reader) error {
		// Advertise capabilities directly in the greeting (via the
		// CAPABILITY response code) so the client caches them immediately
		// and doesn't issue its own CAPABILITY command right after Dial -
		// which this minimal fake server doesn't otherwise answer.
		if err := writeLines(conn, "* PREAUTH [CAPABILITY IMAP4rev1] Fake IMAP ready"); err != nil {
			return err
		}

		tag, rest, err := readCommandLine(br)
		if err != nil {
			return fmt.Errorf("reading SELECT: %w", err)
		}
		if !strings.HasPrefix(strings.ToUpper(rest), "SELECT") {
			return fmt.Errorf("got command %q, want SELECT", rest)
		}
		if err := writeLines(conn,
			"* 9 EXISTS",
			"* 0 RECENT",
			"* FLAGS (\\Deleted \\Seen)",
			"* OK [UIDVALIDITY 1] UIDs valid",
			tag+" OK [READ-WRITE] SELECT completed",
		); err != nil {
			return err
		}

		tag, rest, err = readCommandLine(br)
		if err != nil {
			return fmt.Errorf("reading UID SEARCH: %w", err)
		}
		if !strings.HasPrefix(strings.ToUpper(rest), "UID SEARCH") {
			return fmt.Errorf("got command %q, want UID SEARCH", rest)
		}
		return writeLines(conn,
			"* SEARCH 5 7 9",
			tag+" OK SEARCH completed",
		)
	})

	c, err := client.Dial(addr)
	if err != nil {
		t.Fatalf("client.Dial: %v", err)
	}
	// Terminate just closes the underlying connection with no round-trip -
	// our minimal fake server doesn't script a LOGOUT response.
	defer func() { _ = c.Terminate() }()

	if _, err := c.Select("INBOX", false); err != nil {
		t.Fatalf("Select: %v", err)
	}

	m := &Monitor{client: c}
	unexpected, err := m.deletedUIDsBesides([]uint32{7})
	if err != nil {
		t.Fatalf("deletedUIDsBesides: %v", err)
	}

	want := []uint32{5, 9}
	if len(unexpected) != len(want) {
		t.Fatalf("deletedUIDsBesides = %v, want %v", unexpected, want)
	}
	for i := range want {
		if unexpected[i] != want[i] {
			t.Errorf("deletedUIDsBesides[%d] = %d, want %d", i, unexpected[i], want[i])
		}
	}
}

// uidSearchCtx must return ctx.Err() promptly while the server never answers.
func TestUidSearchCtxCancellation(t *testing.T) {
	gotCommand := make(chan struct{})

	addr := fakeIMAPServer(t, func(conn net.Conn, br *bufio.Reader) error {
		// Advertise capabilities directly in the greeting (via the
		// CAPABILITY response code) so the client caches them immediately
		// and doesn't issue its own CAPABILITY command right after Dial -
		// which this minimal fake server doesn't otherwise answer.
		if err := writeLines(conn, "* PREAUTH [CAPABILITY IMAP4rev1] Fake IMAP ready"); err != nil {
			return err
		}

		tag, rest, err := readCommandLine(br)
		if err != nil {
			return fmt.Errorf("reading SELECT: %w", err)
		}
		if !strings.HasPrefix(strings.ToUpper(rest), "SELECT") {
			return fmt.Errorf("got command %q, want SELECT", rest)
		}
		if err := writeLines(conn, tag+" OK [READ-WRITE] SELECT completed"); err != nil {
			return err
		}

		// Read the UID SEARCH command but never answer it - simulates a
		// hung server. Signal the test so it knows the command was sent
		// before it cancels the context.
		if _, _, err := readCommandLine(br); err != nil {
			return fmt.Errorf("reading UID SEARCH: %w", err)
		}
		close(gotCommand)

		// Block until the test's cleanup closes the connection.
		_, err = br.ReadByte()
		return err
	})

	c, err := client.Dial(addr)
	if err != nil {
		t.Fatalf("client.Dial: %v", err)
	}
	// Terminate just closes the underlying connection with no round-trip -
	// our minimal fake server doesn't script a LOGOUT response.
	defer func() { _ = c.Terminate() }()

	if _, err := c.Select("INBOX", false); err != nil {
		t.Fatalf("Select: %v", err)
	}

	m := &Monitor{client: c}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-gotCommand
		cancel()
	}()

	start := time.Now()
	done := make(chan struct{})
	var searchErr error
	go func() {
		_, searchErr = m.uidSearchCtx(ctx, imap.NewSearchCriteria())
		close(done)
	}()

	select {
	case <-done:
		if elapsed := time.Since(start); elapsed > time.Second {
			t.Errorf("uidSearchCtx took %v to return after cancellation, want prompt return", elapsed)
		}
		if searchErr != context.Canceled {
			t.Errorf("uidSearchCtx error = %v, want context.Canceled", searchErr)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("uidSearchCtx did not return within 5s of context cancellation")
	}
}

// Same as above for fetchMessagesCtx and a hung UID FETCH.
func TestFetchMessagesCtxCancellation(t *testing.T) {
	gotCommand := make(chan struct{})

	addr := fakeIMAPServer(t, func(conn net.Conn, br *bufio.Reader) error {
		// Advertise capabilities directly in the greeting (via the
		// CAPABILITY response code) so the client caches them immediately
		// and doesn't issue its own CAPABILITY command right after Dial -
		// which this minimal fake server doesn't otherwise answer.
		if err := writeLines(conn, "* PREAUTH [CAPABILITY IMAP4rev1] Fake IMAP ready"); err != nil {
			return err
		}

		tag, rest, err := readCommandLine(br)
		if err != nil {
			return fmt.Errorf("reading SELECT: %w", err)
		}
		if !strings.HasPrefix(strings.ToUpper(rest), "SELECT") {
			return fmt.Errorf("got command %q, want SELECT", rest)
		}
		if err := writeLines(conn, tag+" OK [READ-WRITE] SELECT completed"); err != nil {
			return err
		}

		_, rest, err = readCommandLine(br)
		if err != nil {
			return fmt.Errorf("reading UID FETCH: %w", err)
		}
		if !strings.HasPrefix(strings.ToUpper(rest), "UID FETCH") {
			return fmt.Errorf("got command %q, want UID FETCH", rest)
		}
		close(gotCommand)

		_, err = br.ReadByte()
		return err
	})

	c, err := client.Dial(addr)
	if err != nil {
		t.Fatalf("client.Dial: %v", err)
	}
	// Terminate just closes the underlying connection with no round-trip -
	// our minimal fake server doesn't script a LOGOUT response.
	defer func() { _ = c.Terminate() }()

	if _, err := c.Select("INBOX", false); err != nil {
		t.Fatalf("Select: %v", err)
	}

	m := &Monitor{client: c}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-gotCommand
		cancel()
	}()

	seqSet := new(imap.SeqSet)
	seqSet.AddNum(1)
	section := &imap.BodySectionName{}
	items := []imap.FetchItem{imap.FetchEnvelope, imap.FetchUid, section.FetchItem()}

	start := time.Now()
	done := make(chan struct{})
	var fetchErr error
	go func() {
		_, fetchErr = m.fetchMessagesCtx(ctx, seqSet, items, section, 1)
		close(done)
	}()

	select {
	case <-done:
		if elapsed := time.Since(start); elapsed > time.Second {
			t.Errorf("fetchMessagesCtx took %v to return after cancellation, want prompt return", elapsed)
		}
		if fetchErr != context.Canceled {
			t.Errorf("fetchMessagesCtx error = %v, want context.Canceled", fetchErr)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("fetchMessagesCtx did not return within 5s of context cancellation")
	}
}

// No end-to-end ArchiveEmails test: forcing the COPY+STORE+EXPUNGE fallback
// needs a long, brittle scripted exchange, and deletedUIDsBesides is covered
// above; the rest is straight-line log-and-continue.

// fetchMatching only downloads bodies for mail the envelope pass keeps, so
// these predicates must accept everything the old full-fetch-then-filter
// callers kept.
func TestEnvelopeFilters(t *testing.T) {
	cases := []struct {
		name         string
		e            Email
		broker, bnce bool
	}{
		{"broker reply", Email{From: "privacy@acme.com", BrokerID: "acme"}, true, false},
		{"bounce by sender", Email{From: "MAILER-DAEMON@mx.example.org"}, false, true},
		{"bounce by display name", Email{From: "x@example.org", FromName: "Mail Delivery Subsystem"}, false, true},
		{"bounce by subject", Email{From: "x@example.org", Subject: "Undeliverable: Data deletion request"}, false, true},
		{"unrelated", Email{From: "friend@example.org", Subject: "lunch?"}, false, false},
	}
	for _, c := range cases {
		if got := fromBroker(c.e); got != c.broker {
			t.Errorf("%s: fromBroker = %v, want %v", c.name, got, c.broker)
		}
		if got := looksLikeBounce(c.e); got != c.bnce {
			t.Errorf("%s: looksLikeBounce = %v, want %v", c.name, got, c.bnce)
		}
	}
}

// Off port 993 (Proton Bridge 1143), Connect must STARTTLS before LOGIN.
func TestConnectUpgradesWithSTARTTLSBeforeLogin(t *testing.T) {
	ts := httptest.NewTLSServer(http.NotFoundHandler())
	t.Cleanup(ts.Close)
	loginOverTLS := make(chan bool, 1)
	addr := fakeIMAPServer(t, func(conn net.Conn, br *bufio.Reader) error {
		inTLS := false
		if err := writeLines(conn, "* OK [CAPABILITY IMAP4rev1 STARTTLS] ready"); err != nil {
			return err
		}
		for {
			tag, rest, err := readCommandLine(br)
			if err != nil {
				return err
			}
			switch cmd := strings.ToUpper(strings.Fields(rest)[0]); cmd {
			case "CAPABILITY":
				caps := "IMAP4rev1 STARTTLS"
				if inTLS {
					caps = "IMAP4rev1 AUTH=PLAIN"
				}
				err = writeLines(conn, "* CAPABILITY "+caps, tag+" OK done")
			case "STARTTLS":
				if err = writeLines(conn, tag+" OK begin"); err != nil {
					return err
				}
				tlsConn := tls.Server(conn, &tls.Config{Certificates: ts.TLS.Certificates})
				if err = tlsConn.Handshake(); err != nil {
					return err
				}
				conn, br, inTLS = tlsConn, bufio.NewReader(tlsConn), true
			case "LOGIN":
				loginOverTLS <- inTLS
				return writeLines(conn, tag+" OK logged in")
			default:
				err = writeLines(conn, tag+" BAD unexpected "+cmd)
			}
			if err != nil {
				return err
			}
		}
	})
	host, portStr, _ := net.SplitHostPort(addr)
	port, _ := strconv.Atoi(portStr)

	m := NewMonitor(config.InboxConfig{Server: host, Port: port, Email: "jane@example.org", Password: "pw"}, nil)
	if err := m.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if !<-loginOverTLS {
		t.Fatal("LOGIN sent before STARTTLS")
	}
}
