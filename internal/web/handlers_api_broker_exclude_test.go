package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eraser-privacy/eraser/internal/broker"
	"github.com/go-chi/chi/v5"
)

// requestWithBrokerID builds a request carrying a chi URL param, mirroring
// how the real router injects it - these handlers read it via
// chi.URLParam(r, "brokerID"), which is empty on a plain httptest.NewRequest.
func requestWithBrokerID(method, path, brokerID string) *http.Request {
	req := httptest.NewRequest(method, path, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("brokerID", brokerID)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func containsFold(items []string, want string) int {
	n := 0
	for _, i := range items {
		if strings.EqualFold(i, want) {
			n++
		}
	}
	return n
}

func TestHandleAPIExcludeThenIncludeBrokerRoundTrips(t *testing.T) {
	cfg := testConfig()
	s := newTestServer(t, cfg)
	s.configPath = filepath.Join(t.TempDir(), "config.yaml")
	s.brokerDB.Brokers = []broker.Broker{
		{ID: "spokeo", Name: "Spokeo", Email: "privacy@spokeo.com"},
	}

	req := requestWithBrokerID(http.MethodPost, "/api/brokers/spokeo/exclude", "spokeo")
	w := httptest.NewRecorder()
	s.handleAPIExcludeBroker(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("exclude: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	cfg2 := s.getConfig()
	if containsFold(cfg2.Options.ExcludedBrokers, "spokeo") != 1 {
		t.Fatalf("expected spokeo in ExcludedBrokers after exclude, got %+v", cfg2.Options.ExcludedBrokers)
	}
	if got := s.getBrokersWithStatus("default", "", "", "", "", false, false); len(got) != 0 {
		t.Errorf("expected spokeo hidden from default view after exclude, got %+v", got)
	}

	// Re-excluding an already-excluded broker must not duplicate the entry.
	req2 := requestWithBrokerID(http.MethodPost, "/api/brokers/spokeo/exclude", "spokeo")
	w2 := httptest.NewRecorder()
	s.handleAPIExcludeBroker(w2, req2)
	cfg3 := s.getConfig()
	if n := containsFold(cfg3.Options.ExcludedBrokers, "spokeo"); n != 1 {
		t.Errorf("expected exactly one spokeo entry in ExcludedBrokers, got %d: %+v", n, cfg3.Options.ExcludedBrokers)
	}

	// Include reverses it.
	req3 := requestWithBrokerID(http.MethodPost, "/api/brokers/spokeo/include", "spokeo")
	w3 := httptest.NewRecorder()
	s.handleAPIIncludeBroker(w3, req3)

	if w3.Code != http.StatusOK {
		t.Fatalf("include: expected 200, got %d: %s", w3.Code, w3.Body.String())
	}
	cfg4 := s.getConfig()
	if containsFold(cfg4.Options.ExcludedBrokers, "spokeo") != 0 {
		t.Errorf("expected spokeo removed from ExcludedBrokers after include, got %+v", cfg4.Options.ExcludedBrokers)
	}
	if got := s.getBrokersWithStatus("default", "", "", "", "", false, false); len(got) != 1 {
		t.Errorf("expected spokeo back in default view after include, got %+v", got)
	}
}

func TestHandleAPIExcludeBrokerUnknownID(t *testing.T) {
	s := newTestServer(t, testConfig())
	s.configPath = filepath.Join(t.TempDir(), "config.yaml")

	req := requestWithBrokerID(http.MethodPost, "/api/brokers/nonexistent/exclude", "nonexistent")
	w := httptest.NewRecorder()
	s.handleAPIExcludeBroker(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 for unknown broker ID, got %d", w.Code)
	}
}
