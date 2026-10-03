package web

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/drumandbytes/eraser/internal/config"
	"github.com/drumandbytes/eraser/internal/history"
	"github.com/drumandbytes/eraser/internal/schedule"
)

func do(t *testing.T, s *Server, method, target string, hx bool) *httptest.ResponseRecorder {
	t.Helper()
	req := loopbackRequest(method, target, nil)
	if hx {
		req.Header.Set("HX-Request", "true")
	}
	rec := httptest.NewRecorder()
	s.setupRouter().ServeHTTP(rec, req)
	return rec
}

// nilStoreServer has config but no history DB, the state serve is in when
// the database failed to open.
func nilStoreServer(t *testing.T) *Server {
	s := smokeServer(t)
	s.historyStore = nil
	return s
}

func TestDeleteHistoryEndpoints(t *testing.T) {
	s := smokeServer(t)
	now := time.Now()
	for _, st := range []history.Status{history.StatusSent, history.StatusFailed, history.StatusFailed} {
		if err := s.historyStore.Add(&history.Record{ProfileID: "default", BrokerID: "spokeo", BrokerName: "Spokeo", Email: "e", Template: "gdpr", Status: st, SentAt: now}); err != nil {
			t.Fatal(err)
		}
	}

	rec := do(t, s, http.MethodDelete, "/api/history/failed", false)
	if got := decodeJSON(t, rec); rec.Code != http.StatusOK || got["deleted"] != float64(2) {
		t.Fatalf("delete failed: %d %v", rec.Code, got)
	}
	rec = do(t, s, http.MethodDelete, "/api/history", false)
	if got := decodeJSON(t, rec); rec.Code != http.StatusOK || got["deleted"] != float64(1) {
		t.Fatalf("delete all: %d %v", rec.Code, got)
	}

	for _, target := range []string{"/api/history/failed", "/api/history"} {
		if rec := do(t, nilStoreServer(t), http.MethodDelete, target, false); rec.Code != http.StatusInternalServerError {
			t.Errorf("%s without a db: %d", target, rec.Code)
		}
		broken := smokeServer(t)
		_ = broken.historyStore.Close()
		if rec := do(t, broken, http.MethodDelete, target, false); rec.Code != http.StatusInternalServerError {
			t.Errorf("%s with a closed db: %d", target, rec.Code)
		}
	}
}

func TestFormCompleteAndSkip(t *testing.T) {
	for _, c := range []struct {
		action string
		want   history.PipelineStatus
	}{{"complete", history.PipelineConfirmed}, {"skip", history.PipelineRejected}} {
		for _, hx := range []bool{false, true} {
			s := smokeServer(t)
			if err := s.historyStore.Add(&history.Record{ProfileID: "default", BrokerID: "spokeo", BrokerName: "Spokeo", Email: "e", Template: "gdpr", Status: history.StatusSent, SentAt: time.Now()}); err != nil {
				t.Fatal(err)
			}
			rec := do(t, s, http.MethodPost, "/forms/spokeo/"+c.action, hx)
			if hx && (rec.Code != http.StatusOK || rec.Header().Get("HX-Redirect") != "/tasks") {
				t.Errorf("%s hx: %d %q", c.action, rec.Code, rec.Header().Get("HX-Redirect"))
			}
			if !hx && (rec.Code != http.StatusFound || rec.Header().Get("Location") != "/tasks") {
				t.Errorf("%s: %d %q", c.action, rec.Code, rec.Header().Get("Location"))
			}
			recs, _ := s.historyStore.GetAllRequests("default")
			if recs[0].PipelineStatus != c.want {
				t.Errorf("%s: pipeline = %q, want %q", c.action, recs[0].PipelineStatus, c.want)
			}
		}
		if rec := do(t, nilStoreServer(t), http.MethodPost, "/forms/spokeo/"+c.action, false); rec.Code != http.StatusInternalServerError {
			t.Errorf("%s without a db: %d", c.action, rec.Code)
		}
		broken := smokeServer(t)
		_ = broken.historyStore.Close()
		if rec := do(t, broken, http.MethodPost, "/forms/spokeo/"+c.action, false); rec.Code != http.StatusInternalServerError {
			t.Errorf("%s with a closed db: %d", c.action, rec.Code)
		}
	}
}

func TestTaskPages(t *testing.T) {
	s := smokeServer(t)
	task := &history.PendingTask{
		ProfileID: "default", BrokerID: "spokeo", BrokerName: "Spokeo", TaskType: history.TaskCaptcha,
		FormURL:      "https://spokeo.com/optout",
		BrowserState: `{"email":"test@example.com","firstName":"Test","city":""}`,
	}
	if err := s.historyStore.AddPendingTask(task); err != nil {
		t.Fatal(err)
	}
	other := &history.PendingTask{ProfileID: "spouse", BrokerID: "spokeo", BrokerName: "Spokeo", TaskType: history.TaskCaptcha}
	if err := s.historyStore.AddPendingTask(other); err != nil {
		t.Fatal(err)
	}

	rec := do(t, s, http.MethodGet, fmt.Sprintf("/tasks/%d", task.ID), false)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Spokeo") {
		t.Fatalf("detail: %d", rec.Code)
	}
	if sig, bad := bodyLooksLikeTemplateError(rec.Body.String()); bad {
		t.Fatalf("detail template error %q", sig)
	}

	rec = do(t, s, http.MethodGet, fmt.Sprintf("/tasks/%d/helper", task.ID), false)
	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, "First Name") || !strings.Contains(body, "test@example.com") {
		t.Fatalf("helper: %d", rec.Code)
	}
	if sig, bad := bodyLooksLikeTemplateError(body); bad {
		t.Fatalf("helper template error %q", sig)
	}
	if got, _ := s.historyStore.GetPendingTaskByID(task.ID, "default"); !got.OpenedAt.Valid {
		t.Error("opening the helper didn't set opened_at")
	}

	// Another profile's task is invisible.
	for _, p := range []string{"/tasks/%d", "/tasks/%d/helper"} {
		if rec := do(t, s, http.MethodGet, fmt.Sprintf(p, other.ID), false); rec.Code != http.StatusNotFound {
			t.Errorf("%s for another profile's task: %d", p, rec.Code)
		}
	}

	rec = post(t, s, fmt.Sprintf("/tasks/%d/complete", task.ID), url.Values{"status": {"skipped"}})
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != fmt.Sprintf("/tasks/%d/helper", task.ID) {
		t.Fatalf("complete: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	if got, _ := s.historyStore.GetPendingTaskByID(task.ID, "default"); got.Status != "skipped" {
		t.Errorf("status = %q, want the posted one", got.Status)
	}
	post(t, s, fmt.Sprintf("/tasks/%d/complete", task.ID), nil)
	if got, _ := s.historyStore.GetPendingTaskByID(task.ID, "default"); got.Status != "completed" {
		t.Errorf("status = %q, want completed by default", got.Status)
	}
	post(t, s, fmt.Sprintf("/tasks/%d/skip", task.ID), nil)
	if got, _ := s.historyStore.GetPendingTaskByID(task.ID, "default"); got.Status != "skipped" {
		t.Errorf("status = %q after skip", got.Status)
	}
	if got, _ := s.historyStore.GetPendingTaskByID(other.ID, "spouse"); got.Status != "pending" {
		t.Error("completing touched another profile's task")
	}
}

func TestTaskAndReviewEndpointsWithoutDB(t *testing.T) {
	s := nilStoreServer(t)
	for _, c := range []struct{ method, target string }{
		{http.MethodGet, "/tasks/1"},
		{http.MethodGet, "/tasks/1/helper"},
		{http.MethodPost, "/tasks/1/complete"},
		{http.MethodPost, "/tasks/1/skip"},
		{http.MethodGet, "/pipeline/responses/1"},
		{http.MethodPost, "/api/pipeline/responses/1/reviewed"},
	} {
		if rec := do(t, s, c.method, c.target, false); rec.Code != http.StatusInternalServerError {
			t.Errorf("%s %s without a db: %d", c.method, c.target, rec.Code)
		}
	}

	broken := smokeServer(t)
	_ = broken.historyStore.Close()
	for _, target := range []string{"/tasks/1/complete", "/tasks/1/skip"} {
		if rec := do(t, broken, http.MethodPost, target, false); rec.Code != http.StatusInternalServerError {
			t.Errorf("%s with a closed db: %d", target, rec.Code)
		}
	}
}

func TestResponseReviewNotFound(t *testing.T) {
	s := smokeServer(t)
	for _, c := range []struct{ method, target string }{
		{http.MethodGet, "/pipeline/responses/abc"},
		{http.MethodGet, "/pipeline/responses/999"},
		{http.MethodPost, "/api/pipeline/responses/abc/reviewed"},
		{http.MethodPost, "/api/pipeline/responses/999/reviewed"},
	} {
		if rec := do(t, s, c.method, c.target, false); rec.Code != http.StatusNotFound {
			t.Errorf("%s %s: %d, want 404", c.method, c.target, rec.Code)
		}
	}

	resp := &history.BrokerResponse{ProfileID: "default", BrokerID: "spokeo", BrokerName: "Spokeo", ResponseType: "unknown", NeedsReview: true}
	if err := s.historyStore.AddBrokerResponse(resp); err != nil {
		t.Fatal(err)
	}
	rec := do(t, s, http.MethodPost, fmt.Sprintf("/api/pipeline/responses/%d/reviewed", resp.ID), false)
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/tasks" {
		t.Errorf("non-htmx reviewed: %d %q", rec.Code, rec.Header().Get("Location"))
	}
}

func TestSwitchProfile(t *testing.T) {
	s := smokeServer(t)
	cases := []struct {
		id, redirect, wantCookie, wantLoc string
	}{
		{"spouse", "/history", "spouse", "/history"},
		{"ghost", "/history", "", "/history"},
		{"spouse", "//evil.example", "spouse", "/"},
		{"spouse", "https://evil.example", "spouse", "/"},
		{"spouse", "", "spouse", "/"},
	}
	for _, c := range cases {
		rec := post(t, s, "/api/profile", url.Values{"profile_id": {c.id}, "redirect": {c.redirect}})
		if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != c.wantLoc {
			t.Errorf("%+v: %d -> %q", c, rec.Code, rec.Header().Get("Location"))
		}
		var cookie string
		for _, ck := range rec.Result().Cookies() {
			if ck.Name == activeProfileCookie {
				cookie = ck.Value
			}
		}
		if cookie != c.wantCookie {
			t.Errorf("%+v: cookie = %q", c, cookie)
		}
	}

	req := loopbackRequest(http.MethodPost, "/api/profile", strings.NewReader("%zz"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	s.setupRouter().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("malformed form: %d", rec.Code)
	}
}

func inboxServer(t *testing.T) *Server {
	s := smokeServer(t)
	cfg := *s.getConfig()
	// Nothing listens on port 1: Connect fails fast.
	cfg.Inbox = config.InboxConfig{Enabled: true, Server: "127.0.0.1", Port: 1, Email: "test@example.com", Password: "pw", ArchiveFolder: "Eraser"}
	s.config.Store(&cfg)
	return s
}

func TestInboxScanEndpoints(t *testing.T) {
	for _, target := range []string{"/api/inbox/scan", "/api/inbox/rescan"} {
		if rec := do(t, smokeServer(t), http.MethodPost, target, true); !strings.Contains(rec.Body.String(), "not configured") {
			t.Errorf("%s unconfigured: %s", target, rec.Body.String())
		}
		s := inboxServer(t)
		s.historyStore = nil
		if rec := do(t, s, http.MethodPost, target, true); !strings.Contains(rec.Body.String(), "Database not available") {
			t.Errorf("%s without a db: %s", target, rec.Body.String())
		}
		if rec := do(t, inboxServer(t), http.MethodPost, target, true); !strings.Contains(rec.Body.String(), "Failed to connect to inbox") {
			t.Errorf("%s unreachable: %s", target, rec.Body.String())
		}
	}

	// ?clear=true wipes stored replies before rescanning.
	s := inboxServer(t)
	if err := s.historyStore.AddBrokerResponse(&history.BrokerResponse{ProfileID: "default", BrokerID: "spokeo", BrokerName: "Spokeo", ResponseType: "success"}); err != nil {
		t.Fatal(err)
	}
	do(t, s, http.MethodPost, "/api/inbox/rescan?clear=true", true)
	if all, _ := s.historyStore.GetAllBrokerResponses(); len(all) != 0 {
		t.Errorf("clear=true kept %d replies", len(all))
	}
	_ = s.historyStore.Close()
	if rec := do(t, s, http.MethodPost, "/api/inbox/rescan?clear=true", true); !strings.Contains(rec.Body.String(), "Failed to clear responses") {
		t.Errorf("clear on a closed db: %s", rec.Body.String())
	}
}

func TestWriteScanAlertEscapes(t *testing.T) {
	rec := httptest.NewRecorder()
	writeScanAlert(rec, "error", "Oops:", "<b>x</b>")
	if got := rec.Body.String(); !strings.Contains(got, "alert-error") || !strings.Contains(got, "&lt;b&gt;") {
		t.Errorf("alert = %s", got)
	}
}

func TestReclassify(t *testing.T) {
	if rec := do(t, nilStoreServer(t), http.MethodPost, "/api/inbox/reclassify", true); !strings.Contains(rec.Body.String(), "Database not available") {
		t.Errorf("no db: %s", rec.Body.String())
	}
	broken := smokeServer(t)
	_ = broken.historyStore.Close()
	if rec := do(t, broken, http.MethodPost, "/api/inbox/reclassify", true); !strings.Contains(rec.Body.String(), "Failed to get responses") {
		t.Errorf("closed db: %s", rec.Body.String())
	}

	s := inboxServer(t) // unreachable IMAP: the body backfill is skipped, not fatal
	if rec := do(t, s, http.MethodPost, "/api/inbox/reclassify", true); !strings.Contains(rec.Body.String(), "No responses to reclassify") {
		t.Errorf("empty: %s", rec.Body.String())
	}

	add := func(subject, body, typ string) *history.BrokerResponse {
		r := &history.BrokerResponse{ProfileID: "default", BrokerID: "spokeo", BrokerName: "Spokeo",
			ResponseType: typ, EmailSubject: subject, EmailBody: body}
		if err := s.historyStore.AddBrokerResponse(r); err != nil {
			t.Fatal(err)
		}
		return r
	}
	withBody := add("Re: request", "Your data has been deleted from our systems.", "unknown")
	add("Delivery Status Notification (Failure)", "", "unknown")
	add("Re: request", "Thanks.", "unknown")

	rec := do(t, s, http.MethodPost, "/api/inbox/reclassify", true)
	if !strings.Contains(rec.Body.String(), "Reclassification complete") || !strings.Contains(rec.Body.String(), "Processed 3 records") {
		t.Fatalf("reclassify: %s", rec.Body.String())
	}
	got, _ := s.historyStore.GetBrokerResponseByID(withBody.ID, "default")
	if got.ResponseType == "unknown" {
		t.Errorf("a clear deletion confirmation stayed unknown")
	}
}

func TestSchedulerLoopAndCycle(t *testing.T) {
	s := newTestServer(t, testConfig())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s.runScheduler(ctx) // returns once ctx is done; not due with schedule off

	stub := func(path string, err error) {
		cycleExecutable = func() (string, error) { return path, err }
	}
	t.Cleanup(func(orig func() (string, error)) func() { return func() { cycleExecutable = orig } }(cycleExecutable))

	waitIdle := func() {
		t.Helper()
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			s.cycleMu.Lock()
			running := s.cycleRunning
			s.cycleMu.Unlock()
			if !running {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatal("cycle never finished")
	}

	for _, c := range []struct {
		path string
		err  error
	}{{"true", nil}, {"false", nil}, {"", errors.New("no binary")}} {
		stub(c.path, c.err)
		if !s.startCycle() {
			t.Fatalf("startCycle(%q) refused on an idle server", c.path)
		}
		waitIdle()
	}

	s.cycleMu.Lock()
	s.cycleRunning = true
	s.cycleMu.Unlock()
	if s.startCycle() {
		t.Error("started a second cycle while one runs")
	}
	s.cycleMu.Lock()
	s.cycleRunning = false
	s.cycleMu.Unlock()

	s.jobManager.Create(1, "default")
	if s.startCycle() {
		t.Error("started a cycle during a web send job")
	}
}

// A due cycle is started by the loop itself.
func TestSchedulerLoopStartsDueCycle(t *testing.T) {
	orig := cycleExecutable
	t.Cleanup(func() { cycleExecutable = orig })
	started := make(chan struct{}, 1)
	cycleExecutable = func() (string, error) {
		started <- struct{}{}
		return "true", nil
	}

	s := newTestServer(t, testConfig())
	cfg := *s.getConfig()
	cfg.Schedule.Enabled = true
	s.config.Store(&cfg)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.runScheduler(ctx)
	select {
	case <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("a due cycle wasn't started")
	}
}

func TestCycleDueBadStateFile(t *testing.T) {
	s := newTestServer(t, testConfig())
	cfg := *s.getConfig()
	cfg.Schedule.Enabled = true
	s.config.Store(&cfg)
	if err := schedule.SaveState(s.dataDir, schedule.State{LastRun: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.dataDir, "auto-state.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if s.cycleDue() {
		t.Error("an unreadable state file must not trigger a cycle")
	}
}
