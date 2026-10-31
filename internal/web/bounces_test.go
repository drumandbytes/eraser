package web

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/drumandbytes/eraser/internal/broker"
	"github.com/drumandbytes/eraser/internal/history"
	"github.com/drumandbytes/eraser/internal/imaptest"
)

func ndr(addr string) (string, string, string) {
	return "Mail Delivery Subsystem <mailer-daemon@mx.example>", "Delivery Status Notification (Failure)",
		"Delivery to the following recipient failed permanently: " + addr
}

// bounceServer has an inbox holding bounce mail for spokeo, a never-emailed
// broker, an unknown address and an unreadable notice.
func bounceServer(t *testing.T) *Server {
	t.Helper()
	srv := imaptest.Start(t, false)
	for _, addr := range []string{"privacy@spokeo.com", "dpo@acme.example", "nobody@unknown.example"} {
		from, subject, body := ndr(addr)
		srv.Deliver(t, "INBOX", from, subject, body)
	}
	srv.Deliver(t, "INBOX", "postmaster@mx.example", "Undeliverable", "sorry")

	s := inboxServer(t)
	cfg := *s.getConfig()
	cfg.Inbox = srv.Inbox
	s.config.Store(&cfg)
	s.BrokerOverride = s.configPath + ".brokers.yaml"
	s.brokers().Brokers = []broker.Broker{
		{ID: "spokeo", Name: "Spokeo", Email: "privacy@spokeo.com", Region: "us"},
		{ID: "acme-eu", Name: "Acme EU", Email: "dpo@acme.example", Region: "eu"},
	}
	if err := s.brokers().Save(s.BrokerOverride); err != nil {
		t.Fatal(err)
	}
	if err := s.historyStore.Add(&history.Record{ProfileID: "default", BrokerID: "spokeo", BrokerName: "Spokeo", Email: "privacy@spokeo.com",
		Template: "gdpr", Status: history.StatusSent, SentAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestBounceCheckAndClear(t *testing.T) {
	s := bounceServer(t)

	body := do(t, s, http.MethodPost, "/api/inbox/bounces", true).Body.String()
	if !strings.Contains(body, "Found 4 bounced email(s)") || !strings.Contains(body, "privacy@spokeo.com") || !strings.Contains(body, "Clear 1 dead address") {
		t.Fatalf("scan:\n%s", body)
	}
	if strings.Contains(body, "dpo@acme.example") {
		t.Error("a bounce for a broker we never emailed must not be offered for clearing")
	}
	if !strings.Contains(body, "3 other bounce(s) were ignored") {
		t.Errorf("skipped note missing:\n%s", body)
	}
	if s.brokers().FindByID("spokeo").Email == "" {
		t.Fatal("the scan alone must not change anything")
	}

	body = do(t, s, http.MethodPost, "/api/inbox/bounces/clear", true).Body.String()
	if !strings.Contains(body, "Cleared 1 broker email address(es)") {
		t.Fatalf("clear:\n%s", body)
	}
	spokeo := s.brokers().FindByID("spokeo")
	if spokeo == nil || spokeo.Email != "" || !strings.Contains(spokeo.Notes, "undeliverable") {
		t.Errorf("spokeo after clear = %+v", spokeo)
	}
	if body = do(t, s, http.MethodPost, "/api/inbox/bounces/clear", true).Body.String(); !strings.Contains(body, "No broker email addresses need to be cleared") {
		t.Errorf("second clear:\n%s", body)
	}
}

func TestBounceCheckRefusals(t *testing.T) {
	if body := do(t, smokeServer(t), http.MethodPost, "/api/inbox/bounces", true).Body.String(); !strings.Contains(body, "not configured") {
		t.Errorf("unconfigured: %s", body)
	}
	noDB := inboxServer(t)
	noDB.historyStore = nil
	if body := do(t, noDB, http.MethodPost, "/api/inbox/bounces", true).Body.String(); !strings.Contains(body, "Database not available") {
		t.Errorf("no db: %s", body)
	}
	if body := do(t, inboxServer(t), http.MethodPost, "/api/inbox/bounces", true).Body.String(); !strings.Contains(body, "Bounce check failed") {
		t.Errorf("unreachable: %s", body)
	}

	empty := inboxServer(t)
	srv := imaptest.Start(t, false)
	cfg := *empty.getConfig()
	cfg.Inbox = srv.Inbox
	empty.config.Store(&cfg)
	if body := do(t, empty, http.MethodPost, "/api/inbox/bounces", true).Body.String(); !strings.Contains(body, "No bounced emails found") {
		t.Errorf("empty inbox: %s", body)
	}
}

func TestBounceCheckWithNothingToClearAndClearFailures(t *testing.T) {
	// Only an unknown-address bounce: reported, but nothing to clear.
	srv := imaptest.Start(t, false)
	from, subject, body := ndr("nobody@unknown.example")
	srv.Deliver(t, "INBOX", from, subject, body)
	s := inboxServer(t)
	cfg := *s.getConfig()
	cfg.Inbox = srv.Inbox
	s.config.Store(&cfg)
	if got := do(t, s, http.MethodPost, "/api/inbox/bounces", true).Body.String(); !strings.Contains(got, "None of them need action") || strings.Contains(got, "Clear ") {
		t.Errorf("no dead addresses:\n%s", got)
	}

	// A list that can't be edited: the clear fails loudly and changes nothing.
	b := bounceServer(t)
	b.BrokerOverride = b.configPath + ".missing-brokers.yaml"
	if got := do(t, b, http.MethodPost, "/api/inbox/bounces/clear", true).Body.String(); !strings.Contains(got, "clear the addresses") {
		t.Errorf("unwritable list:\n%s", got)
	}
	if b.brokers().FindByID("spokeo").Email == "" {
		t.Error("a failed clear must not change the loaded list")
	}
}
