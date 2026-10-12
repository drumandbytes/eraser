package web

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/drumandbytes/eraser/internal/history"
)

func pipelineTestServer(t *testing.T) *Server {
	t.Helper()
	s := newTestServer(t, testConfig("default", "other"))
	store, err := history.NewStore(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	s.historyStore = store
	return s
}

func pipelineReply(t *testing.T, s *Server, r history.BrokerResponse) history.BrokerResponse {
	t.Helper()
	if r.ProfileID == "" {
		r.ProfileID = "default"
	}
	if r.ReceivedAt.IsZero() {
		r.ReceivedAt = time.Now()
	}
	if err := s.historyStore.AddBrokerResponse(&r); err != nil {
		t.Fatal(err)
	}
	return r
}

func pipelineGET(s *Server, path string, hx bool) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodGet, path, nil)
	if hx {
		r.Header.Set("HX-Request", "true")
	}
	w := httptest.NewRecorder()
	s.handlePipeline(w, r)
	return w
}

func TestPipelineEighteenReviewsWithoutTaskRecords(t *testing.T) {
	s := pipelineTestServer(t)
	for i := range 18 {
		pipelineReply(t, s, history.BrokerResponse{BrokerID: fmt.Sprint(i), BrokerName: fmt.Sprintf("Review broker %02d", i), ResponseType: "pending", NeedsReview: true})
	}
	stats := s.getPipelineStats("default")
	if stats.PendingTasks != 18 || stats.NeedsReview != 18 {
		t.Fatalf("stats=%+v", stats)
	}
	for _, hx := range []bool{false, true} {
		w := pipelineGET(s, "/pipeline", hx)
		body := w.Body.String()
		if w.Code != 200 || strings.Count(body, `<article class="pipeline-result"`) != 18 || strings.Contains(body, "No action required") {
			t.Fatalf("status=%d; result rows=%d", w.Code, strings.Count(body, `<article class="pipeline-result"`))
		}
		if hx && strings.Contains(body, "<!DOCTYPE") {
			t.Error("HTMX returned a full document")
		}
	}
	w := httptest.NewRecorder()
	s.handleTasks(w, httptest.NewRequest("GET", "/tasks", nil))
	if w.Code != 200 || strings.Count(w.Body.String(), `class="action-item `) != 18 {
		t.Fatal("Action Needed and Pipeline disagree")
	}
}

func TestPipelineActionsDeduplicateFormsAndIncludeReviewTasks(t *testing.T) {
	s := pipelineTestServer(t)
	for _, id := range []string{"standalone", "linked"} {
		pipelineReply(t, s, history.BrokerResponse{BrokerID: id, BrokerName: id, ResponseType: "form_required", FormURL: "https://example.com/form"})
	}
	for _, task := range []history.PendingTask{
		{BrokerID: "linked", BrokerName: "Linked form", TaskType: history.TaskManualForm, Notes: "Find this special note"},
		{BrokerID: "review-task", BrokerName: "Review task", TaskType: history.TaskReview},
	} {
		if err := s.historyStore.AddPendingTask(&task); err != nil {
			t.Fatal(err)
		}
	}
	pipelineReply(t, s, history.BrokerResponse{BrokerID: "review-response", BrokerName: "Review successful reply", ResponseType: "success", NeedsReview: true, EmailBody: "A body-only needle for ÅSE"})
	a, err := s.historyStore.GetActionItems("default")
	if err != nil || a.Total() != 4 || a.FormsToFill() != 2 || a.NeedsReview() != 2 {
		t.Fatalf("actions=%+v err=%v", a, err)
	}
	for _, tc := range []struct {
		query string
		rows  int
	}{
		{"", 4}, {"?needs_review=true", 2}, {"?q=special+note", 1}, {"?q=body-only", 1}, {"?q=åse", 1}, {"?needs_review=true&q=special", 0},
	} {
		w := pipelineGET(s, "/pipeline"+tc.query, true)
		if got := strings.Count(w.Body.String(), `<article class="pipeline-result"`); w.Code != 200 || got != tc.rows {
			t.Errorf("%s: status=%d rows=%d want=%d", tc.query, w.Code, got, tc.rows)
		}
	}
}

func TestPipelineResponseSearchFiltersBeforePaging(t *testing.T) {
	s := pipelineTestServer(t)
	now := time.Now()
	for i := range 62 {
		r := history.BrokerResponse{BrokerID: fmt.Sprint(i), BrokerName: fmt.Sprintf("Broker %02d", i), ResponseType: "pending", EmailSubject: "Request update", ReceivedAt: now.Add(time.Duration(i) * time.Minute)}
		if i == 0 {
			r.ResponseType = "no_data"
			r.EmailBody = "We hold no data. Distinctive 100%_literal body match."
		}
		pipelineReply(t, s, r)
	}
	pipelineReply(t, s, history.BrokerResponse{ProfileID: "other", BrokerID: "private", BrokerName: "Private other profile", ResponseType: "no_data", EmailBody: "Distinctive 100%_literal body match."})
	for _, tc := range []struct {
		q    string
		rows int
		want string
	}{
		{"?view=responses", 50, "62 results"},
		{"?view=responses&page=2", 12, "Page 2 of 2"},
		{"?view=responses&q=DISTINCTIVE", 1, "Distinctive"},
		{"?view=responses&type=no_data&q=100%25_literal", 1, "No data held"},
		{"?view=responses&type=rejected&q=Distinctive", 0, "No matching results"},
		{"?view=responses&type=no_data&needs_review=true", 0, "No matching results"},
		{"?view=responses&page=10000000000000000", 50, "62 results"},
	} {
		w := pipelineGET(s, "/pipeline"+tc.q, true)
		body := w.Body.String()
		if w.Code != 200 || strings.Count(body, `<article class="pipeline-result"`) != tc.rows || !strings.Contains(body, tc.want) || strings.Contains(body, "Private other profile") {
			t.Errorf("%s: status=%d rows=%d, expected %d and %q", tc.q, w.Code, strings.Count(body, `<article class="pipeline-result"`), tc.rows, tc.want)
		}
	}
	w := pipelineGET(s, "/pipeline?view=responses&type=no_data", false)
	if !strings.Contains(w.Body.String(), `name="type"`) || !strings.Contains(w.Body.String(), `value="no_data" selected`) {
		t.Error("response control doesn't submit or restore its selection")
	}
	api := httptest.NewRecorder()
	s.handleAPIResponses(api, httptest.NewRequest("GET", "/api/pipeline/responses?type=no_data&q=Distinctive", nil))
	if api.Code != 200 || !strings.Contains(api.Body.String(), "Broker 00") || strings.Contains(api.Body.String(), "Broker 61") {
		t.Fatal("response API ignores filter/search")
	}
}

func TestPipelineStageCardsUseLatestRequestAndRestoreURLs(t *testing.T) {
	s := pipelineTestServer(t)
	for _, r := range []history.Record{
		{BrokerID: "confirmed", BrokerName: "Confirmed broker", Status: history.StatusSent, SentAt: time.Now().Add(-time.Hour)},
		{BrokerID: "confirmed", BrokerName: "Confirmed broker", Status: history.StatusSent, SentAt: time.Now()},
		{BrokerID: "waiting", BrokerName: "Waiting broker", Status: history.StatusSent, SentAt: time.Now()},
	} {
		if err := s.historyStore.Add(&r); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.historyStore.UpdatePipelineStatus("default", "confirmed", history.PipelineConfirmed); err != nil {
		t.Fatal(err)
	}
	w := pipelineGET(s, "/pipeline?status=confirmed", true)
	body := w.Body.String()
	if w.Code != 200 || strings.Count(body, `<article class="pipeline-result"`) != 1 || !strings.Contains(body, "Confirmed broker") || strings.Contains(body, "Waiting broker") {
		t.Fatal("stage drill-down does not match its count")
	}
	if !strings.Contains(w.Header().Get("HX-Push-Url"), "status=confirmed") || !strings.Contains(body, `aria-current="true"`) {
		t.Error("selection not reflected in URL/card")
	}
	r := httptest.NewRequest("GET", "/pipeline?status=confirmed", nil)
	r.Header.Set("HX-Request", "true")
	r.Header.Set("HX-History-Restore-Request", "true")
	restored := httptest.NewRecorder()
	s.handlePipeline(restored, r)
	if !strings.Contains(strings.ToLower(restored.Body.String()), "<!doctype html>") {
		t.Error("history restore requires full document")
	}
}

func TestPipelineReadFailureIsNotAnEmptyState(t *testing.T) {
	s := pipelineTestServer(t)
	_ = s.historyStore.Close()
	for _, hx := range []bool{false, true} {
		w := pipelineGET(s, "/pipeline", hx)
		if w.Code != 500 || !strings.Contains(w.Body.String(), "Unable to load pipeline") || strings.Contains(w.Body.String(), "No action required") {
			t.Fatal("read failure shown as all clear")
		}
	}
}

func TestResponsePreviewIsPlainText(t *testing.T) {
	got := responsePreview("<script>bad()</script><p>We hold <strong>no data</strong>.</p>", "")
	if strings.Contains(got, "bad()") || strings.Contains(got, "<") || !strings.Contains(got, "no data") {
		t.Fatalf("preview=%q", got)
	}
}

func TestResponsePreviewUnicodeMatch(t *testing.T) {
	// Case folding can change the UTF-8 byte length (Ⱥ -> ⱥ). Snippet offsets
	// must still use rune positions and never slice beyond the original text.
	body := strings.Repeat("Ⱥ", 250) + " ÅSE's records"
	if got := responsePreview(body, "åse"); !strings.Contains(got, "ÅSE's records") {
		t.Fatalf("preview missed Unicode search: %q", got)
	}
}
