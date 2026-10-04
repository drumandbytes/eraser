// Package imaptest runs go-imap's in-memory server for tests: STARTTLS on
// loopback (config.TLSFor skips verification there), any login name with
// password "password".
package imaptest

import (
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/backend"
	"github.com/emersion/go-imap/backend/memory"
	"github.com/emersion/go-imap/server"

	"github.com/drumandbytes/eraser/internal/config"
)

type Server struct {
	// Inbox connects to this server's INBOX, archiving to "Eraser".
	Inbox   config.InboxConfig
	backend *memory.Backend
}

// Start serves an empty INBOX (plus the memory backend's one sample message).
// move adds MOVE support, which the memory backend lacks, so archiving can
// take either the MOVE or the COPY+EXPUNGE path.
func Start(t testing.TB, move bool) *Server {
	t.Helper()
	s := &Server{backend: memory.New()}
	ts := httptest.NewTLSServer(http.NotFoundHandler())
	t.Cleanup(ts.Close)

	srv := server.New(&wrapper{Backend: s.backend, move: move})
	srv.TLSConfig = ts.TLS.Clone()
	srv.ErrorLog = log.New(io.Discard, "", 0)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })

	s.Inbox = config.InboxConfig{Enabled: true, Server: "127.0.0.1", Port: ln.Addr().(*net.TCPAddr).Port,
		Email: "username", Password: "password", Folder: "INBOX", ArchiveFolder: "Eraser"}
	return s
}

// Mailbox returns the named mailbox, creating it if needed.
func (s *Server) Mailbox(t testing.TB, name string) *memory.Mailbox {
	t.Helper()
	u, err := s.backend.Login(nil, "username", "password")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := u.GetMailbox(name); err != nil {
		if err := u.CreateMailbox(name); err != nil {
			t.Fatal(err)
		}
	}
	mb, err := u.GetMailbox(name)
	if err != nil {
		t.Fatal(err)
	}
	return mb.(*memory.Mailbox)
}

// Deliver puts a plain-text message dated now into folder.
func (s *Server) Deliver(t testing.TB, folder, from, subject, body string) {
	t.Helper()
	msg := fmt.Sprintf("From: %s\r\nTo: username@example.com\r\nSubject: %s\r\nDate: %s\r\nMessage-ID: <%d@test>\r\nContent-Type: text/plain\r\n\r\n%s",
		from, subject, time.Now().Format(time.RFC1123Z), time.Now().UnixNano(), body)
	if err := s.Mailbox(t, folder).CreateMessage(nil, time.Now(), strings.NewReader(msg)); err != nil {
		t.Fatal(err)
	}
}

// MailboxNames lists every mailbox.
func (s *Server) MailboxNames(t testing.TB) []string {
	t.Helper()
	u, err := s.backend.Login(nil, "username", "password")
	if err != nil {
		t.Fatal(err)
	}
	boxes, err := u.ListMailboxes(false)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, len(boxes))
	for i, b := range boxes {
		names[i] = b.Name()
	}
	return names
}

func (s *Server) Count(t testing.TB, folder string) int {
	t.Helper()
	return len(s.Mailbox(t, folder).Messages)
}

// wrapper lets any login name in (so two profiles' inboxes can differ) and
// optionally adds MOVE.
type wrapper struct {
	*memory.Backend
	move bool
}

func (b *wrapper) Login(ci *imap.ConnInfo, _, pass string) (backend.User, error) {
	u, err := b.Backend.Login(ci, "username", pass)
	if err != nil || !b.move {
		return u, err
	}
	return moveUser{u}, nil
}

type moveUser struct{ backend.User }

func (u moveUser) GetMailbox(name string) (backend.Mailbox, error) {
	mb, err := u.User.GetMailbox(name)
	if err != nil {
		return nil, err
	}
	return moveMailbox{mb}, nil
}

type moveMailbox struct{ backend.Mailbox }

func (m moveMailbox) MoveMessages(uid bool, seq *imap.SeqSet, dest string) error {
	if err := m.CopyMessages(uid, seq, dest); err != nil {
		return err
	}
	if err := m.UpdateMessagesFlags(uid, seq, imap.AddFlags, []string{imap.DeletedFlag}); err != nil {
		return err
	}
	return m.Expunge()
}
