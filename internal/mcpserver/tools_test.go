package mcpserver

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/drumandbytes/eraser/internal/browser"
	"github.com/drumandbytes/eraser/internal/config"
	"github.com/drumandbytes/eraser/internal/history"
)

// stepPage is a form with a second step: a click moves to a new page that still
// has a control, which is what a "progress" outcome needs.
type stepPage struct{ fakePage }

func (f *stepPage) Act(context.Context, string, string, string, string) (*browser.Snapshot, error) {
	f.snap.ID = "after"
	f.snap.Text = "Step 2: confirm your email"
	f.snap.Controls = []browser.Control{{Ref: "n0", Label: "Next"}}
	v := f.snap
	return &v, nil
}

// openedBatch returns a service with broker:one's form open on page.
func openedBatch(t *testing.T, page pageSession) (*Service, string) {
	t.Helper()
	s := testService(t)
	s.newPage = func(browser.BrowserConfig, *config.Profile, browser.URLPolicy) (pageSession, error) { return page, nil }
	id := beginTestBatch(t, s, prepareInput{TaskIDs: []string{"broker:one"}})
	if _, err := s.openTask(context.Background(), openInput{id, "broker:one"}); err != nil {
		t.Fatal(err)
	}
	return s, id
}

func formPage() *fakePage {
	return &fakePage{snap: browser.Snapshot{ID: "before", URL: "https://8.8.8.8/form", Text: "Remove my information", Controls: []browser.Control{{Ref: "e0", Label: "Email"}, {Ref: "e1", Label: "Submit"}}}}
}

func TestActValidation(t *testing.T) {
	s, id := openedBatch(t, formPage())
	ctx := context.Background()
	act := func(in actionInput) error {
		in.BatchID, in.TaskID = id, "broker:one"
		_, err := s.act(ctx, in)
		return err
	}
	for name, in := range map[string]actionInput{
		"stale snapshot":         {SnapshotID: "old", Ref: "e1", Action: "click"},
		"unknown action":         {SnapshotID: "before", Ref: "e1", Action: "hover"},
		"unknown control":        {SnapshotID: "before", Ref: "e9", Action: "click"},
		"oversized value":        {SnapshotID: "before", Ref: "e0", Action: "fill", Value: strings.Repeat("x", 4001)},
		"value and profile":      {SnapshotID: "before", Ref: "e0", Action: "fill", Value: "x", ProfileField: "email"},
		"missing profile field":  {SnapshotID: "before", Ref: "e0", Action: "fill", ProfileField: "phone"},
		"invented profile field": {SnapshotID: "before", Ref: "e0", Action: "fill", ProfileField: "ssn"},
	} {
		if err := act(in); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if err := act(actionInput{SnapshotID: "before", Ref: "e0", Action: "fill", ProfileField: "email"}); err != nil {
		t.Fatalf("filling a configured profile field: %v", err)
	}
	attempts, err := s.store.GetAutomationAttempts("alice", subject(s.pages["broker:one"].task))
	if err != nil || len(attempts) != 1 || attempts[0].State != "progress" {
		t.Fatalf("fill should be journaled as progress: %+v %v", attempts, err)
	}
}

func TestActRefusesBlockedPage(t *testing.T) {
	page := formPage()
	page.snap.Blocker = "Solve the CAPTCHA in the open Chrome window"
	s, id := openedBatch(t, page)
	_, err := s.act(context.Background(), actionInput{BatchID: id, TaskID: "broker:one", SnapshotID: "before", Ref: "e1", Action: "click"})
	if err == nil || !strings.Contains(err.Error(), "needs user") {
		t.Fatalf("blocked page: %v", err)
	}
}

func TestRecordResultOutcomes(t *testing.T) {
	ctx := context.Background()
	record := func(s *Service, id, outcome, evidence string) (map[string]any, error) {
		return s.recordResult(ctx, resultInput{id, "broker:one", s.pages["broker:one"].page.ID, outcome, evidence})
	}

	s, id := openedBatch(t, formPage())
	for name, in := range map[string][2]string{
		"unknown outcome": {"done", "Thank you for waiting"},
		"too little":      {"submitted", "ok"},
		"no change yet":   {"progress", "Remove my information"},
	} {
		if _, err := record(s, id, in[0], in[1]); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	out, err := record(s, id, "needs_user", "A login form appeared")
	if err != nil {
		t.Fatal(err)
	}
	task, _ := s.store.GetPendingTaskByID(out["ui_task_id"].(int64), "alice")
	if !task.NeedsReview() || task.ReviewReason != "needs_user" || task.Actor != "mcp:" {
		t.Fatalf("needs_user task = %+v", task)
	}
	if _, err := record(s, id, "unknown", "The page just went blank"); err != nil {
		t.Fatal(err)
	}
	task, _ = s.store.GetPendingTaskByID(out["ui_task_id"].(int64), "alice")
	if task.ReviewReason != "outcome_unknown" {
		t.Fatalf("unknown outcome should wait for review: %+v", task)
	}

	// A click that lands on a visible next step is progress, not completion.
	s, id = openedBatch(t, &stepPage{*formPage()})
	if _, err := s.act(ctx, actionInput{BatchID: id, TaskID: "broker:one", SnapshotID: "before", Ref: "e1", Action: "click"}); err != nil {
		t.Fatal(err)
	}
	out, err = record(s, id, "progress", "Step 2: confirm your email")
	if err != nil {
		t.Fatalf("progress: %v", err)
	}
	task, _ = s.store.GetPendingTaskByID(out["ui_task_id"].(int64), "alice")
	if task.Status != "pending" || task.NeedsReview() {
		t.Fatalf("progress should keep the task open and out of review: %+v", task)
	}
}

func TestOpenConfirmationTaskClaimsTheAction(t *testing.T) {
	page := formPage()
	s := testService(t)
	s.newPage = func(browser.BrowserConfig, *config.Profile, browser.URLPolicy) (pageSession, error) { return page, nil }
	task := &history.PendingTask{ProfileID: "alice", BrokerID: "one", BrokerName: "One", TaskType: history.TaskConfirm, FormURL: "https://8.8.8.8/confirm"}
	if err := s.store.AddPendingTask(task); err != nil {
		t.Fatal(err)
	}
	taskID := "task:" + itoa(task.ID)
	id := beginTestBatch(t, s, prepareInput{TaskIDs: []string{taskID}})
	ctx := context.Background()
	out, err := s.openTask(ctx, openInput{id, taskID})
	if err != nil || out["action_key"] == "" {
		t.Fatalf("open: %v %v", out, err)
	}
	tp := s.pages[taskID]
	if tp == nil || !tp.pendingAction || tp.lastAction == "" {
		t.Fatalf("a confirmation link acts when opened, so it must be journaled: %+v", tp)
	}
	if again, err := s.openTask(ctx, openInput{id, taskID}); err != nil || again["page"] == nil {
		t.Fatalf("reopening an open task should just observe it: %v %v", again, err)
	}
}

func TestScreenshotTool(t *testing.T) {
	page := formPage()
	page.snap.Screenshot = "mcp_shot.png"
	s, id := openedBatch(t, page)
	dir := s.screenshotDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	png := []byte("\x89PNG\r\n\x1a\nfake")
	if err := os.WriteFile(filepath.Join(dir, "mcp_shot.png"), png, 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := s.screenshot(context.Background(), openInput{id, "broker:one"})
	if err != nil || string(out["_image"].([]byte)) != string(png) {
		t.Fatalf("screenshot: %v %v", out, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "mcp_shot.png"), make([]byte, 5<<20), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.screenshot(context.Background(), openInput{id, "broker:one"}); err == nil || !strings.Contains(err.Error(), "4 MiB") {
		t.Fatalf("oversized screenshot: %v", err)
	}
	s.pages["broker:one"].page.Screenshot = ""
	if _, err := s.screenshot(context.Background(), openInput{id, "broker:one"}); err == nil {
		t.Fatal("screenshot without one taken")
	}
}
