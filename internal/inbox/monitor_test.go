package inbox

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
	gomail "github.com/emersion/go-message/mail"
)

// buildMIMEMessage builds a single-part text/plain MIME message (valid
// RFC 5322 + MIME headers, no multipart wrapping needed since mail.NewReader
// synthesizes one) with the given body. Content-Transfer-Encoding is set to
// "8bit" so the bytes pass through undecoded, making the resulting decoded
// body directly comparable to the input.
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

// TestParseMessageCapsOversizedMIMEPart is the highest-priority regression
// test for the maxMIMEPartBytes fix: a broker-reply email is
// attacker-influenced (anyone can mail the monitored inbox), so a MIME part
// body read without a bound would let a huge attachment or body force
// unbounded memory growth. This constructs a synthetic oversized MIME
// message directly (no live/mocked IMAP server needed - parseMessage takes
// an *imap.Message whose Body map we can populate ourselves) and asserts
// the parsed Email.Body never exceeds the cap.
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
// Fake IMAP server helpers, used by the tests below to exercise code paths
// that need a real *client.Client (a concrete type from emersion/go-imap
// with no interface seam - see notes on TestDeletedUIDsBesides and
// TestUidSearchCtxCancellation for why this route was chosen and what was
// left untested).
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

// TestDeletedUIDsBesides exercises the deletedUIDsBesides helper added by
// the Expunge over-deletion fix, against a minimal scripted IMAP server on
// a real loopback TCP connection.
//
// Why a real server and not a mock: Monitor.client is the concrete
// *client.Client type from github.com/emersion/go-imap/client - there is no
// interface seam to substitute a fake implementation, and several of the
// client's methods (UidSearch included) refuse to run unless the client
// believes it is in the IMAP "selected" state, which is only reachable by
// actually driving the wire protocol. go-imap's own client tests solve this
// with an unexported test-only state setter that isn't visible outside
// their package, so from internal/inbox the only path in is a real
// loopback listener speaking just enough IMAP: a PREAUTH greeting (skips
// LOGIN) followed by a scripted SELECT and UID SEARCH exchange.
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

// TestUidSearchCtxCancellation checks that uidSearchCtx returns promptly
// with ctx.Err() when the context is canceled while the underlying IMAP
// command is still in flight, mirroring the existing cancellation pattern
// WatchForNewEmails uses for its blocking IDLE call. The fake server reads
// the UID SEARCH command and then deliberately never responds, simulating
// a slow/hung server; the test asserts uidSearchCtx does not block on that
// non-response.
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

// TestFetchMessagesCtxCancellation is the fetchMessagesCtx analogue of
// TestUidSearchCtxCancellation above: the fake server acknowledges SELECT,
// reads the UID FETCH command, and then hangs, verifying fetchMessagesCtx
// also returns promptly on context cancellation instead of blocking on the
// UidFetch call.
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

// Note on ArchiveEmails / the Expunge over-deletion fix as a whole:
// deletedUIDsBesides (tested above) is the new logic the fix introduced.
// A full end-to-end ArchiveEmails test (forcing the UID MOVE to fail so the
// COPY+STORE+EXPUNGE fallback runs, then asserting the expunge-count
// mismatch warning is logged) is possible with the same fake-server
// approach but requires scripting a longer, more failure-prone exchange
// (capability negotiation so the real client.UidMove sends a wire command
// instead of silently rerouting to its own internal fallback, then COPY,
// STORE, the deletedUIDsBesides SEARCH, and EXPUNGE in sequence, plus
// capturing the log package's default output). Given that risk/complexity
// for coverage that deletedUIDsBesides's own test already exercises at the
// unit level, it was left out here rather than force it; the code path is
// still straight-line log-and-continue logic with no branching this test
// suite doesn't already cover in isolation.
