package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/drumandbytes/eraser/internal/broker"
)

func TestGetBrokersWithStatusRecipientFilters(t *testing.T) {
	s := newTestServer(t, testConfig())
	s.brokerDB.Brokers = []broker.Broker{
		{ID: "alpha", Name: "Alpha", Email: "privacy@alpha.example", Region: "us", Category: "marketing"},
		{ID: "beta", Name: "Beta", Email: "privacy@beta.example", Region: "eu", Category: "people-search"},
		{ID: "noemail", Name: "No Email", Region: "eu", Category: "people-search"},
	}

	got := s.getBrokersWithStatus("default", "alpha", "marketing", "us", "all", []string{"alpha", "beta"}, []string{"beta"}, false, false)
	if len(got) != 1 || got[0].ID != "alpha" {
		t.Fatalf("filtered brokers = %+v, want alpha", got)
	}
}

func TestHandleAPISendAllRejectsUnknownBrokerID(t *testing.T) {
	cfg := testConfig()
	cfg.Email.Provider = "smtp"
	s := newTestServer(t, cfg)
	s.brokerDB.Brokers = []broker.Broker{{ID: "alpha", Name: "Alpha", Email: "privacy@alpha.example", Region: "us"}}

	form := url.Values{"broker_ids": {"missing"}}
	req := httptest.NewRequest(http.MethodPost, "/api/send-all", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	s.handleAPISendAll(rec, req)

	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "Unknown broker IDs: missing") {
		t.Fatalf("response = %d %q", rec.Code, rec.Body.String())
	}
}
