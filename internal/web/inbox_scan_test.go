package web

import (
	"net/http"
	"strings"
	"testing"

	"github.com/drumandbytes/eraser/internal/config"
	"github.com/drumandbytes/eraser/internal/history"
	"github.com/drumandbytes/eraser/internal/imaptest"
)

// imapServer serves INBOX with the given (from, subject, body) messages.
func imapServer(t *testing.T, mails ...[3]string) config.InboxConfig {
	t.Helper()
	srv := imaptest.Start(t, false)
	for _, m := range mails {
		srv.Deliver(t, "INBOX", m[0], m[1], m[2])
	}
	return srv.Inbox
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
