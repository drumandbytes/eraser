package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/drumandbytes/eraser/internal/broker"
	"github.com/drumandbytes/eraser/internal/history"
)

func parityServer(t *testing.T) *Server {
	t.Helper()
	s := newTestServer(t, testConfig())
	s.configPath = filepath.Join(t.TempDir(), "config.yaml")
	s.brokerDB.Store(&broker.BrokerDatabase{Brokers: []broker.Broker{
		{ID: "acme", Name: "Acme Data", Email: "privacy@acme.example", Region: "eu"},
	}})
	store, err := history.NewStore(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	s.historyStore = store
	if err := store.Add(&history.Record{ProfileID: "default", BrokerID: "acme", BrokerName: "Acme Data",
		Email: "privacy@acme.example", Template: "gdpr", Status: history.StatusSent, SentAt: time.Now().AddDate(0, -2, 0)}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	return s
}

// The web download is the same report `eraser export` writes.
func TestHandleExportDownloadsActiveProfileReport(t *testing.T) {
	s := parityServer(t)

	rec := httptest.NewRecorder()
	s.handleExport(rec, httptest.NewRequest(http.MethodGet, "/export?format=json", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Header().Get("Content-Disposition"), `attachment; filename="eraser-evidence-default-`) {
		t.Fatalf("got %d, disposition %q", rec.Code, rec.Header().Get("Content-Disposition"))
	}
	var rep struct {
		Brokers []struct {
			BrokerID     string `json:"broker_id"`
			PastDeadline bool   `json:"past_deadline"`
		} `json:"brokers"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &rep); err != nil || len(rep.Brokers) != 1 || rep.Brokers[0].BrokerID != "acme" || !rep.Brokers[0].PastDeadline {
		t.Fatalf("report = %+v (%v)", rep, err)
	}

	html := httptest.NewRecorder()
	s.handleExport(html, httptest.NewRequest(http.MethodGet, "/export", nil))
	if html.Code != http.StatusOK || !strings.Contains(html.Body.String(), "Acme Data") {
		t.Fatalf("html export: %d", html.Code)
	}
	for _, q := range []string{"format=xml", "since=last-week"} {
		bad := httptest.NewRecorder()
		s.handleExport(bad, httptest.NewRequest(http.MethodGet, "/export?"+q, nil))
		if bad.Code != http.StatusBadRequest {
			t.Errorf("%s: got %d, want 400", q, bad.Code)
		}
	}
}

// Same as `eraser mark-bounced`: the latest sent record becomes failed.
func TestHandleAPIMarkBounced(t *testing.T) {
	s := parityServer(t)

	req := withURLParam(httptest.NewRequest(http.MethodPost, "/api/brokers/acme/mark-bounced", nil), "brokerID", "acme")
	rec := httptest.NewRecorder()
	s.handleAPIMarkBounced(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Failed") {
		t.Fatalf("got %d: %s", rec.Code, rec.Body.String())
	}
	st, err := s.historyStore.GetBrokerStatus("default", "acme")
	if err != nil || st.Status != history.StatusFailed {
		t.Fatalf("status = %+v (%v), want failed", st, err)
	}

	missing := httptest.NewRecorder()
	s.handleAPIMarkBounced(missing, withURLParam(httptest.NewRequest(http.MethodPost, "/", nil), "brokerID", "nope"))
	if missing.Code != http.StatusNotFound {
		t.Errorf("unknown broker: got %d, want 404", missing.Code)
	}
}

// Same as `eraser update-brokers`, plus the running server switches to the
// new list without a restart.
func TestHandleSettingsBrokersUpdateReloadsList(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := parityServer(t)

	var yaml strings.Builder
	yaml.WriteString("brokers:\n")
	n := broker.MinSaneBrokerCount + 3
	for i := 0; i < n; i++ {
		fmt.Fprintf(&yaml, "  - {id: b%d, name: B%d, email: b%d@example.com, region: eu}\n", i, i, i)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(yaml.String()))
	}))
	defer srv.Close()
	s.brokerUpdateURL = srv.URL

	rec := httptest.NewRecorder()
	s.handleSettingsBrokersUpdate(rec, httptest.NewRequest(http.MethodPost, "/settings/brokers/update", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Broker list updated") {
		t.Fatalf("got %d: %s", rec.Code, rec.Body.String())
	}
	if got := len(s.brokers().Brokers); got != n {
		t.Fatalf("server still uses %d brokers, want the downloaded %d", got, n)
	}
}
