package web

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-imap/backend/memory"
	"github.com/emersion/go-imap/server"

	"github.com/drumandbytes/eraser/internal/config"
	"github.com/drumandbytes/eraser/internal/history"
)

type quietLog struct{}

func (quietLog) Printf(string, ...interface{}) {}
func (quietLog) Println(...interface{})        {}

// imapServer serves go-imap's in-memory backend over STARTTLS on loopback
// with the given (from, subject, body) messages in INBOX.
func imapServer(t *testing.T, mails ...[3]string) config.InboxConfig {
	t.Helper()
	be := memory.New()
	u, _ := be.Login(nil, "username", "password")
	mb, _ := u.GetMailbox("INBOX")
	for i, m := range mails {
		msg := fmt.Sprintf("From: %s\r\nTo: test@example.com\r\nSubject: %s\r\nDate: %s\r\nMessage-ID: <%d@t>\r\nContent-Type: text/plain\r\n\r\n%s",
			m[0], m[1], time.Now().Format(time.RFC1123Z), i, m[2])
		if err := mb.(*memory.Mailbox).CreateMessage(nil, time.Now(), strings.NewReader(msg)); err != nil {
			t.Fatal(err)
		}
	}
	ts := httptest.NewTLSServer(http.NotFoundHandler())
	t.Cleanup(ts.Close)
	srv := server.New(be)
	srv.TLSConfig = ts.TLS.Clone()
	srv.ErrorLog = quietLog{}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	_, portStr, _ := net.SplitHostPort(ln.Addr().String())
	port, _ := strconv.Atoi(portStr)
	return config.InboxConfig{Enabled: true, Server: "127.0.0.1", Port: port, Email: "username", Password: "password",
		Folder: "INBOX", ArchiveFolder: "Eraser"}
}

func withInbox(t *testing.T, in config.InboxConfig) *Server {
	s := smokeServer(t)
	cfg := *s.getConfig()
	cfg.Inbox = in
	s.config.Store(&cfg)
	return s
}

func TestInboxScanFindsReplies(t *testing.T) {
	s := withInbox(t, imapServer(t,
		[3]string{"privacy@spokeo.com", "Re: Erasure request", "We have deleted your personal data from our systems."},
		[3]string{"dpo@acme.example", "Your request", "Please complete our opt-out form at https://acme.example/optout"},
		[3]string{"friend@example.com", "Lunch?", "Tomorrow?"},
	))

	rec := do(t, s, http.MethodPost, "/api/inbox/scan", true)
	body := rec.Body.String()
	if !strings.Contains(body, "Scan complete!") || !strings.Contains(body, "Found 2 broker emails") {
		t.Fatalf("scan: %s", body)
	}
	if !strings.Contains(body, `New: <span class="font-semibold">2</span>`) {
		t.Errorf("new count missing: %s", body)
	}
	if got, _ := s.historyStore.GetBrokerResponses("default", "", false, 10); len(got) != 2 {
		t.Errorf("stored %d replies", len(got))
	}

	rec = do(t, s, http.MethodPost, "/api/inbox/rescan", true)
	if !strings.Contains(rec.Body.String(), `Updated: <span class="font-semibold">2</span>`) {
		t.Errorf("rescan: %s", rec.Body.String())
	}
}

func TestInboxScanNothingFound(t *testing.T) {
	s := withInbox(t, imapServer(t, [3]string{"friend@example.com", "Lunch?", "Tomorrow?"}))
	if body := do(t, s, http.MethodPost, "/api/inbox/scan", true).Body.String(); !strings.Contains(body, "No broker emails found") {
		t.Errorf("scan: %s", body)
	}
}

// Replies stored before bodies were kept get them back from IMAP and are
// then classified on the full text.
func TestReclassifyBackfillsBodies(t *testing.T) {
	s := withInbox(t, imapServer(t,
		[3]string{"privacy@spokeo.com", "Re: Erasure request", "We have deleted your personal data from our systems."},
	))
	resp := &history.BrokerResponse{ProfileID: "default", BrokerID: "spokeo", BrokerName: "Spokeo",
		ResponseType: "unknown", EmailSubject: "Re: Erasure request"}
	if err := s.historyStore.AddBrokerResponse(resp); err != nil {
		t.Fatal(err)
	}

	body := do(t, s, http.MethodPost, "/api/inbox/reclassify", true).Body.String()
	if !strings.Contains(body, "Reclassification complete") {
		t.Fatalf("reclassify: %s", body)
	}
	got, _ := s.historyStore.GetBrokerResponseByID(resp.ID, "default")
	if got.EmailBody == "" || got.ResponseType != "success" {
		t.Errorf("after backfill = %+v", got)
	}
}
