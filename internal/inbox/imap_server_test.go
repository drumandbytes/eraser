package inbox

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/backend"
	"github.com/emersion/go-imap/backend/memory"
	"github.com/emersion/go-imap/server"

	"github.com/drumandbytes/eraser/internal/broker"
	"github.com/drumandbytes/eraser/internal/config"
	"github.com/drumandbytes/eraser/internal/history"
)

// testBackend is go-imap's in-memory backend, optionally with MOVE (the
// memory backend has none, so ArchiveEmails falls back to COPY+EXPUNGE).
type testBackend struct {
	*memory.Backend
	move bool
}

func (b *testBackend) Login(ci *imap.ConnInfo, user, pass string) (backend.User, error) {
	u, err := b.Backend.Login(ci, user, pass)
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

type imapFixture struct {
	cfg     config.InboxConfig
	be      *testBackend
	brokers []broker.Broker
}

// newIMAPFixture serves the backend over STARTTLS on a loopback port (TLS
// verification is skipped for loopback, see config.TLSFor).
func newIMAPFixture(t *testing.T, move bool) *imapFixture {
	t.Helper()
	be := &testBackend{Backend: memory.New(), move: move}
	ts := httptest.NewTLSServer(http.NotFoundHandler())
	t.Cleanup(ts.Close)

	srv := server.New(be)
	srv.TLSConfig = ts.TLS.Clone()
	srv.ErrorLog = discardLogger{}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })

	_, portStr, _ := net.SplitHostPort(ln.Addr().String())
	port, _ := strconv.Atoi(portStr)
	return &imapFixture{
		be: be,
		cfg: config.InboxConfig{Enabled: true, Server: "127.0.0.1", Port: port,
			Email: "username", Password: "password", Folder: "INBOX", ArchiveFolder: "Eraser"},
		brokers: []broker.Broker{
			{ID: "acme", Name: "Acme", Email: "privacy@acme.example"},
			{ID: "globex", Name: "Globex", Website: "https://www.globex.example/privacy"},
		},
	}
}

type discardLogger struct{}

func (discardLogger) Printf(string, ...interface{}) {}
func (discardLogger) Println(...interface{})        {}

func (f *imapFixture) mailbox(t *testing.T, name string) backend.Mailbox {
	t.Helper()
	u, err := f.be.Backend.Login(nil, "username", "password")
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
	return mb
}

func (f *imapFixture) deliver(t *testing.T, folder, from, subject, body string) {
	t.Helper()
	msg := fmt.Sprintf("From: %s\r\nTo: username@example.com\r\nSubject: %s\r\nDate: %s\r\nMessage-ID: <%d@test>\r\nContent-Type: text/plain\r\n\r\n%s",
		from, subject, time.Now().Format(time.RFC1123Z), time.Now().UnixNano(), body)
	if err := f.mailbox(t, folder).(*memory.Mailbox).CreateMessage(nil, time.Now(), strings.NewReader(msg)); err != nil {
		t.Fatal(err)
	}
}

func (f *imapFixture) count(t *testing.T, folder string) int {
	t.Helper()
	return len(f.mailbox(t, folder).(*memory.Mailbox).Messages)
}

func (f *imapFixture) connect(t *testing.T) *Monitor {
	t.Helper()
	m := NewMonitor(f.cfg, f.brokers)
	if err := m.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(func() { _ = m.Disconnect() })
	return m
}

func TestConnectErrors(t *testing.T) {
	f := newIMAPFixture(t, false)
	bad := f.cfg
	bad.Password = "wrong"
	if err := NewMonitor(bad, nil).Connect(context.Background()); err == nil || !strings.Contains(err.Error(), "failed to login") {
		t.Errorf("wrong password: %v", err)
	}
	nobody := f.cfg
	nobody.Port = 1
	if err := NewMonitor(nobody, nil).Connect(context.Background()); err == nil {
		t.Error("connected to a closed port")
	}
	if err := NewMonitor(f.cfg, nil).Disconnect(); err != nil {
		t.Errorf("Disconnect without a connection: %v", err)
	}
}

// A server without STARTTLS must never get the password.
func TestConnectRefusesPlaintextServer(t *testing.T) {
	srv := server.New(memory.New())
	srv.AllowInsecureAuth = true
	srv.ErrorLog = discardLogger{}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	_, portStr, _ := net.SplitHostPort(ln.Addr().String())
	port, _ := strconv.Atoi(portStr)

	err = NewMonitor(config.InboxConfig{Server: "127.0.0.1", Port: port, Email: "username", Password: "password"}, nil).Connect(context.Background())
	if err == nil || !strings.Contains(err.Error(), "does not offer STARTTLS") {
		t.Errorf("err = %v", err)
	}
}

func TestFetchBrokerAndBounceEmails(t *testing.T) {
	f := newIMAPFixture(t, false)
	f.deliver(t, "INBOX", "Acme Privacy <privacy@acme.example>", "Re: Erasure request", "Your data has been deleted.")
	f.deliver(t, "INBOX", "dpo@globex.example", "Your request", "Please use our form.")
	f.deliver(t, "INBOX", "friend@example.com", "Lunch?", "Tomorrow?")
	f.deliver(t, "INBOX", "Mail Delivery Subsystem <mailer-daemon@googlemail.com>", "Delivery Status Notification (Failure)",
		"Delivery to the following recipient failed permanently: gone@deadbroker.example")
	m := f.connect(t)

	emails, err := m.FetchBrokerEmails(context.Background(), 7)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]Email{}
	for _, e := range emails {
		got[e.BrokerID] = e
	}
	if len(emails) != 2 || got["acme"].Body == "" || got["globex"].BrokerName != "Globex" {
		t.Fatalf("broker emails = %+v", emails)
	}
	if got["acme"].FromName != "Acme Privacy" || got["acme"].MessageID == "" {
		t.Errorf("acme envelope = %+v", got["acme"])
	}

	bounces, err := m.FetchBounceEmails(context.Background(), 7)
	if err != nil || len(bounces) != 1 {
		t.Fatalf("bounces = %+v, %v", bounces, err)
	}
	if got := ExtractBouncedRecipient(&bounces[0]); got != "gone@deadbroker.example" {
		t.Errorf("bounced recipient = %q", got)
	}

	if _, err := m.FetchBrokerEmailsFromFolder(context.Background(), "Nope", 7); err == nil {
		t.Error("fetching a missing folder should fail")
	}
	f.mailbox(t, "Empty")
	if got, err := m.FetchBrokerEmailsFromFolder(context.Background(), "Empty", 7); err != nil || got != nil {
		t.Errorf("empty folder = %v, %v", got, err)
	}

}

func TestNotConnectedErrors(t *testing.T) {
	m := NewMonitor(config.InboxConfig{}, nil)
	if _, err := m.FetchBrokerEmails(context.Background(), 1); err == nil {
		t.Error("FetchBrokerEmails")
	}
	if err := m.WatchForNewEmails(context.Background(), func(Email) {}); err == nil {
		t.Error("WatchForNewEmails")
	}
	if err := m.EnsureFolderExists("x"); err == nil {
		t.Error("EnsureFolderExists")
	}
	if err := m.ArchiveEmails([]uint32{1}, "x"); err == nil {
		t.Error("ArchiveEmails")
	}
}

func TestEnsureFolderExists(t *testing.T) {
	f := newIMAPFixture(t, false)
	m := f.connect(t)
	if err := m.EnsureFolderExists("Eraser"); err != nil {
		t.Fatal(err)
	}
	if err := m.EnsureFolderExists("eraser"); err != nil { // case-insensitive match, no duplicate
		t.Fatal(err)
	}
	u, _ := f.be.Backend.Login(nil, "username", "password")
	boxes, _ := u.ListMailboxes(false)
	if len(boxes) != 2 {
		t.Errorf("mailboxes = %d, want INBOX + Eraser", len(boxes))
	}
}

// ScanAndStore end to end: classify, store under the profile that emailed
// the broker, then archive (MOVE when offered, COPY+EXPUNGE otherwise).
func TestScanAndStoreArchives(t *testing.T) {
	for _, move := range []bool{true, false} {
		t.Run(fmt.Sprintf("move=%v", move), func(t *testing.T) {
			f := newIMAPFixture(t, move)
			f.cfg.AutoArchive = true
			f.deliver(t, "INBOX", "privacy@acme.example", "Re: Erasure request", "We have deleted your personal data from our systems.")
			f.deliver(t, "INBOX", "dpo@globex.example", "Action required", "Please complete our opt-out form at https://www.globex.example/optout")
			before := f.count(t, "INBOX")
			m := f.connect(t)

			store, err := history.NewStore(filepath.Join(t.TempDir(), "history.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = store.Close() }()
			if err := store.Add(&history.Record{ProfileID: "jane", BrokerID: "acme", BrokerName: "Acme", Email: "privacy@acme.example",
				Template: "gdpr", Status: history.StatusSent, SentAt: time.Now()}); err != nil {
				t.Fatal(err)
			}

			res, err := m.ScanAndStore(context.Background(), store, ScanOptions{Days: 7, IncludeArchive: true})
			if err != nil {
				t.Fatal(err)
			}
			if res.Summary.Total != 2 || len(res.New) != 2 || res.Archived != 2 || res.Summary.Success != 1 || res.Summary.FormRequired != 1 {
				t.Fatalf("result = %+v", res)
			}
			if f.count(t, "INBOX") != before-2 || f.count(t, "Eraser") != 2 {
				t.Errorf("INBOX %d (was %d), Eraser %d", f.count(t, "INBOX"), before, f.count(t, "Eraser"))
			}
			if jane, _ := store.GetBrokerResponses("jane", "", false, 10); len(jane) != 1 || jane[0].BrokerID != "acme" {
				t.Errorf("acme reply not attributed to jane: %+v", jane)
			}

			// A rescan reads the archive folder and finds the same replies again.
			res, err = m.ScanAndStore(context.Background(), store, ScanOptions{Days: 7, IncludeArchive: true, Reclassify: true})
			if err != nil || res.Updated != 2 || len(res.New) != 0 {
				t.Errorf("rescan = %+v, %v", res, err)
			}
		})
	}
}

func TestScanAndStoreFetchError(t *testing.T) {
	f := newIMAPFixture(t, false)
	f.cfg.Folder = "Missing"
	m := f.connect(t)
	store, _ := history.NewStore(filepath.Join(t.TempDir(), "history.db"))
	defer func() { _ = store.Close() }()
	if _, err := m.ScanAndStore(context.Background(), store, ScanOptions{Days: 7}); err == nil {
		t.Error("expected an error for a missing folder")
	}
}

func TestArchiveEmailsNothingToDo(t *testing.T) {
	f := newIMAPFixture(t, false)
	m := f.connect(t)
	if err := m.ArchiveEmails(nil, "Eraser"); err != nil {
		t.Errorf("empty archive: %v", err)
	}
	if err := m.ArchiveEmails([]uint32{6}, "NoSuchFolder"); err == nil {
		t.Error("archiving into a missing folder should fail")
	}
}
