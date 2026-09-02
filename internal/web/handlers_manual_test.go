package web

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/drumandbytes/eraser/internal/broker"
	"github.com/drumandbytes/eraser/internal/config"
	"github.com/drumandbytes/eraser/internal/history"
)

func manualTestServer(t *testing.T) *Server {
	t.Helper()
	s := newTestServer(t, &config.Config{
		Profile: config.Profile{FirstName: "Jane", LastName: "Doe", Email: "jane@example.com"},
		Options: config.Options{Template: "gdpr", SendMode: "manual"},
	})
	s.configPath = filepath.Join(t.TempDir(), "config.yaml")
	s.brokerDB = &broker.BrokerDatabase{Brokers: []broker.Broker{
		{ID: "acme", Name: "Acme Data", Email: "privacy@acme.example", Region: "us", Category: "people-search"},
		{ID: "noemail", Name: "No Email Co", Region: "us", Category: "marketing"},
	}}
	store, err := history.NewStore(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	s.historyStore = store
	return s
}

func TestSetupCompleteManual(t *testing.T) {
	s := newTestServer(t, testConfig())
	s.configPath = filepath.Join(t.TempDir(), "config.yaml")

	id, err := s.sessions.Create()
	if err != nil {
		t.Fatal(err)
	}
	s.sessions.Update(id, func(sess *Session) {
		sess.Profile = config.Profile{FirstName: "Jane", LastName: "Doe", Email: "jane@example.com"}
		sess.Email = config.Email{Provider: "manual"}
		sess.ManualSend = true
		sess.Step = "complete"
	})

	req := httptest.NewRequest(http.MethodGet, "/setup/complete", nil)
	req.AddCookie(&http.Cookie{Name: "eraser_session", Value: id})
	w := httptest.NewRecorder()
	s.handleSetupComplete(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	cfg := s.getConfig()
	if !cfg.IsManualSend() {
		t.Errorf("saved config send_mode = %q, want manual", cfg.Options.SendMode)
	}
	if cfg.Email.Provider != "" {
		t.Errorf("manual setup should leave email empty, got provider %q", cfg.Email.Provider)
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("manual config should validate: %v", err)
	}
}

func TestBrokersPageManualMode(t *testing.T) {
	s := manualTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/brokers", nil)
	w := httptest.NewRecorder()
	s.handleBrokers(w, req)

	body := w.Body.String()
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	if !strings.Contains(body, "Manual mode.") {
		t.Error("brokers page missing the manual-mode banner")
	}
	if !strings.Contains(body, "/brokers/acme/email") {
		t.Error("brokers page missing the per-row Email link")
	}
	if strings.Contains(body, `id="send-all-btn"`) {
		t.Error("Send to All button should be hidden in manual mode")
	}
}

func TestBrokerEmailPageAndMarkSent(t *testing.T) {
	s := manualTestServer(t)

	// The email page renders the removal request.
	req := httptest.NewRequest(http.MethodGet, "/brokers/acme/email", nil)
	req = withURLParam(req, "brokerID", "acme")
	w := httptest.NewRecorder()
	s.handleBrokerEmail(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("email page: want 200, got %d: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	for _, want := range []string{"Acme Data", "privacy@acme.example", "Article 17", "mailto:privacy@acme.example"} {
		if !strings.Contains(body, want) {
			t.Errorf("email page missing %q", want)
		}
	}

	// Mark sent records a manual request and returns the status badge.
	req = httptest.NewRequest(http.MethodPost, "/api/brokers/acme/mark-sent", nil)
	req = withURLParam(req, "brokerID", "acme")
	w = httptest.NewRecorder()
	s.handleAPIMarkSent(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("mark-sent: want 200, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Sent") {
		t.Errorf("mark-sent response should contain the Sent badge, got %q", w.Body.String())
	}

	recs, err := s.historyStore.GetAllRequests(config.DefaultProfileID)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 || recs[0].BrokerID != "acme" || recs[0].SentMethod != "manual" || recs[0].Status != history.StatusSent {
		t.Errorf("history after mark-sent = %+v", recs)
	}
}
