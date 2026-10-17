package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/drumandbytes/eraser/internal/history"
)

func TestAutomationApprovalPageAndApprove(t *testing.T) {
	s := pipelineTestServer(t)
	preview := `{"profile_id":"default","profile_fields":{"email":"ada@example.com"},"tasks":[{"broker_name":"Acme","kind":"form","url":"https://acme.example/optout"}],"emails":[{"broker_id":"beta","to":"privacy@beta.example","from":"ada@example.com","subject":"Erasure request","body":"Please delete my data."}]}`
	if err := s.historyStore.CreateBatchApproval(history.BatchApproval{PlanID: "plan-1", ProfileID: "default", Digest: "d", Preview: preview, ExpiresAt: time.Now().Add(15 * time.Minute)}); err != nil {
		t.Fatal(err)
	}

	rec := do(t, s, http.MethodGet, "/automation", false)
	body := rec.Body.String()
	for _, want := range []string{"plan-1", "Acme", "privacy@beta.example", "Please delete my data."} {
		if rec.Code != 200 || !strings.Contains(body, want) {
			t.Fatalf("automation page missing %q (status %d)", want, rec.Code)
		}
	}

	form := url.Values{"plan_id": {"plan-1"}}.Encode()
	req := loopbackRequest(http.MethodPost, "/automation/approve", strings.NewReader(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	rec = httptest.NewRecorder()
	s.setupRouter().ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("approve: status %d body %s", rec.Code, rec.Body.String())
	}
	if err := s.historyStore.ConsumeBatchApproval("plan-1", "d"); err != nil {
		t.Fatalf("approval not recorded: %v", err)
	}

	req = loopbackRequest(http.MethodPost, "/automation/approve", strings.NewReader(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	rec = httptest.NewRecorder()
	s.setupRouter().ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("second approve: status %d, want 409", rec.Code)
	}
}

func TestAutomationApproveRequiresSameOriginBrowserRequest(t *testing.T) {
	s := pipelineTestServer(t)
	if err := s.historyStore.CreateBatchApproval(history.BatchApproval{PlanID: "p", ProfileID: "default", Digest: "d", Preview: "{}", ExpiresAt: time.Now().Add(time.Minute)}); err != nil {
		t.Fatal(err)
	}
	form := url.Values{"plan_id": {"p"}}.Encode()
	for name, header := range map[string]string{"curl": "", "cross-site": "cross-site"} {
		req := loopbackRequest(http.MethodPost, "/automation/approve", strings.NewReader(form))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if header != "" {
			req.Header.Set("Sec-Fetch-Site", header)
		}
		rec := httptest.NewRecorder()
		s.setupRouter().ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s: status %d, want 403", name, rec.Code)
		}
	}
	if err := s.historyStore.ConsumeBatchApproval("p", "d"); err == nil {
		t.Fatal("a blocked request approved the batch")
	}
}
