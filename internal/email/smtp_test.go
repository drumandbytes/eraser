package email

import (
	"context"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/drumandbytes/eraser/internal/config"
)

// hangingSMTPServer accepts one connection and then never writes anything -
// not even the SMTP greeting smtp.NewClient blocks reading for - simulating
// a server that accepted the TCP handshake but is otherwise unresponsive
// (a hung connection, a firewall black-holing the session, etc).
func hangingSMTPServer(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return // listener closed by t.Cleanup
		}
		<-context.Background().Done() // never happens - just holds the conn open
		_ = conn.Close()
	}()

	return ln.Addr().String()
}

// TestSendRespectsContextDeadline is the regression test for the bug fixed
// in send(): net/smtp has no context support, so before this fix a hung
// SMTP server (one that accepts the connection and then never responds)
// made Send block forever, ignoring the 30s timeout every caller sets on
// ctx. A short deadline here must still make Send return promptly.
func TestSendRespectsContextDeadline(t *testing.T) {
	addr := hangingSMTPServer(t)
	host, port := splitHostPortForTest(t, addr)

	sender := NewSMTPSender(config.SMTPConfig{Host: host, Port: port, UseTLS: false}, "from@example.com")

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	start := time.Now()
	result := sender.Send(ctx, Message{To: "to@example.com", From: "from@example.com", Subject: "s", Body: "b"})
	elapsed := time.Since(start)

	if result.Success {
		t.Fatal("expected failure against a hung server, got success")
	}
	if elapsed > 2*time.Second {
		t.Fatalf("Send took %s against a 300ms deadline - context deadline is not being enforced", elapsed)
	}
}

// TestSendRespectsContextCancellation covers the other half: a job's Cancel
// button cancels the context with no deadline at all. Send must still
// return promptly once cancelled, not hang until some other timeout - this
// is the watcher-goroutine-closes-the-connection path, distinct from the
// deadline/SetDeadline path TestSendRespectsContextDeadline exercises.
func TestSendRespectsContextCancellation(t *testing.T) {
	addr := hangingSMTPServer(t)
	host, port := splitHostPortForTest(t, addr)

	sender := NewSMTPSender(config.SMTPConfig{Host: host, Port: port, UseTLS: false}, "from@example.com")

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(200*time.Millisecond, cancel)

	start := time.Now()
	result := sender.Send(ctx, Message{To: "to@example.com", From: "from@example.com", Subject: "s", Body: "b"})
	elapsed := time.Since(start)

	if result.Success {
		t.Fatal("expected failure against a hung server, got success")
	}
	if elapsed > 2*time.Second {
		t.Fatalf("Send took %s after cancellation at 200ms - cancellation is not unblocking the connection", elapsed)
	}
}

func splitHostPortForTest(t *testing.T, addr string) (string, int) {
	t.Helper()
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("SplitHostPort(%q): %v", addr, err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("parse port %q: %v", portStr, err)
	}
	return host, port
}
