package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/drumandbytes/eraser/internal/broker"
	"github.com/drumandbytes/eraser/internal/browser"
	"github.com/drumandbytes/eraser/internal/config"
	"github.com/drumandbytes/eraser/internal/history"
	"github.com/drumandbytes/eraser/internal/smtptest"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func testService(t *testing.T) *Service {
	t.Helper()
	dir := t.TempDir()
	bp := filepath.Join(dir, "brokers.yaml")
	db := &broker.BrokerDatabase{Brokers: []broker.Broker{{ID: "one", Name: "One", Email: "privacy@one.example", Website: "https://8.8.8.8", OptOutURL: "https://8.8.8.8/form", Region: "eu"}, {ID: "two", Name: "Two", Email: "privacy@two.example", Website: "https://8.8.8.8", Region: "eu"}}}
	if err := db.Save(bp); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{Profiles: []config.NamedProfile{{ID: "alice", Profile: config.Profile{FirstName: "Alice", LastName: "Test", Email: "alice@example.com"}}, {ID: "bob", Profile: config.Profile{FirstName: "Bob", LastName: "Test", Email: "bob@example.com"}}}, Email: config.EmailConfig{SMTP: config.SMTPConfig{Password: "SECRET-DO-NOT-RETURN"}}, Options: config.Options{SendMode: "manual", Template: "gdpr", BrokerFile: bp, DailySendLimit: 1}}
	cp := filepath.Join(dir, "config.yaml")
	if err := config.Save(cp, cfg); err != nil {
		t.Fatal(err)
	}
	s, err := New(Options{ConfigPath: cp, ProfileID: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// approvedRetry runs the two-step retry_action flow: request, out-of-band
// approval, then the approved call. It also checks the unapproved paths refuse.
func approvedRetry(t *testing.T, s *Service, in retryInput) {
	t.Helper()
	ctx := context.Background()
	first, err := s.retry(ctx, in)
	if err != nil || first["approval_required"] != true {
		t.Fatalf("first retry call: %v %v", first, err)
	}
	id := first["approval_id"].(string)
	in.ApprovalID = id
	if _, err := s.retry(ctx, in); err == nil {
		t.Fatal("retry released before approval")
	}
	if err := s.store.ApproveBatch(id, "test"); err != nil {
		t.Fatal(err)
	}
	other := in
	other.Reason = "A different reason than the one that was approved."
	if _, err := s.retry(ctx, other); err == nil {
		t.Fatal("approval reused for a different retry")
	}
	if out, err := s.retry(ctx, in); err != nil || out["retry_allowed"] != true {
		t.Fatalf("approved retry: %v %v", out, err)
	}
	if _, err := s.retry(ctx, in); err == nil {
		t.Fatal("approval spent twice")
	}
}

// approveTestPlan stands in for the web UI or `eraser mcp approve`.
func approveTestPlan(t *testing.T, s *Service, planID string) {
	t.Helper()
	if err := s.store.ApproveBatch(planID, "test"); err != nil {
		t.Fatal(err)
	}
}

func beginTestBatch(t *testing.T, s *Service, in prepareInput) string {
	t.Helper()
	preview, err := s.prepare(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	approveTestPlan(t, s, preview["plan_id"].(string))
	b, err := s.begin(context.Background(), beginInput{PlanID: preview["plan_id"].(string)})
	if err != nil {
		t.Fatal(err)
	}
	return b["batch_id"].(string)
}

func TestMCPTransportDiscoverySchemasAndCredentialRedaction(t *testing.T) {
	s := testService(t)
	ctx := context.Background()
	a, b := mcp.NewInMemoryTransports()
	ss, err := s.MCP.Connect(ctx, a, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ss.Close() }()
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	cs, err := client.Connect(ctx, b, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cs.Close() }()
	if info := cs.InitializeResult().ServerInfo; info.Name != "eraser" || info.Version != Version || !strings.Contains(info.Title, "experimental") {
		t.Fatalf("server info = %+v", info)
	}
	list, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	for _, tool := range list.Tools {
		found[tool.Name] = true
		if tool.Name == "begin_batch" && tool.Annotations.ReadOnlyHint {
			t.Fatal("batch authorization incorrectly read-only")
		}
	}
	for _, name := range []string{"list_tasks", "prepare_batch", "begin_batch", "open_task", "browser_action", "record_result", "send_request", "get_screenshot"} {
		if !found[name] {
			t.Errorf("missing %s", name)
		}
	}
	for _, call := range []struct {
		name string
		args map[string]any
	}{{"get_status", map[string]any{}}, {"prepare_batch", map[string]any{"broker_ids": []string{"one"}}}} {
		r, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: call.name, Arguments: call.args})
		if err != nil || r.IsError {
			t.Fatalf("%s: %+v %v", call.name, r, err)
		}
		j, _ := json.Marshal(r)
		if strings.Contains(string(j), "SECRET-DO-NOT-RETURN") {
			t.Fatal("credential in tool response")
		}
	}
	r, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "open_task", Arguments: map[string]any{"batch_id": "invented", "task_id": "broker:one"}})
	if err != nil {
		t.Fatal(err)
	}
	if !r.IsError {
		t.Fatal("unapproved tool call accepted")
	}
}

func TestBatchScopeExpiryAndConfigChanges(t *testing.T) {
	s := testService(t)
	ctx := context.Background()
	if _, err := s.prepare(ctx, prepareInput{}); err == nil {
		t.Fatal("empty batch accepted")
	}
	foreign := &history.PendingTask{ProfileID: "bob", BrokerID: "one", BrokerName: "One", TaskType: history.TaskManualForm, FormURL: "https://8.8.8.8/form"}
	if err := s.store.AddPendingTask(foreign); err != nil {
		t.Fatal(err)
	}
	if _, err := s.getTask(ctx, taskInput{TaskID: fmt.Sprintf("task:%d", foreign.ID)}); err == nil {
		t.Fatal("foreign profile exposed")
	}
	id := beginTestBatch(t, s, prepareInput{TaskIDs: []string{"broker:one"}})
	if _, err := s.send(ctx, sendInput{id, "two"}); err == nil {
		t.Fatal("out-of-batch broker accepted")
	}
	cfg, err := config.Load(s.opts.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Profiles[0].Email = "changed@example.com"
	if err := config.Save(s.opts.ConfigPath, cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := s.authorize(id); err == nil {
		t.Fatal("changed profile kept its approval")
	}
	s.endBatch()
	if _, err := s.authorize(id); err == nil {
		t.Fatal("ended batch still authorized")
	}
	preview, err := s.prepare(ctx, prepareInput{BrokerIDs: []string{"one"}})
	if err != nil {
		t.Fatal(err)
	}
	pid := preview["plan_id"].(string)
	s.plans[pid].Created = time.Now().Add(-16 * time.Minute)
	if _, err := s.begin(ctx, beginInput{pid}); err == nil {
		t.Fatal("unapproved preview began")
	}
	approveTestPlan(t, s, pid)
	if _, err := s.begin(ctx, beginInput{pid}); err == nil {
		t.Fatal("expired preview approved")
	}
}

type fakePage struct {
	snap     browser.Snapshot
	closed   bool
	fail     bool
	openFail bool
}

func (f *fakePage) Open(context.Context, string) (*browser.Snapshot, error) {
	if f.openFail {
		return nil, fmt.Errorf("timeout")
	}
	v := f.snap
	return &v, nil
}
func (f *fakePage) Observe(context.Context) (*browser.Snapshot, error) { v := f.snap; return &v, nil }
func (f *fakePage) Act(context.Context, string, string, string, string) (*browser.Snapshot, error) {
	if f.fail {
		return nil, fmt.Errorf("connection lost")
	}
	f.snap.ID = "after"
	f.snap.Text = "Your request has been received."
	f.snap.Controls = nil
	v := f.snap
	return &v, nil
}
func (f *fakePage) Close() { f.closed = true }

func TestSubmissionEvidenceAndUnknownAttemptRecovery(t *testing.T) {
	s := testService(t)
	ctx := context.Background()
	fake := &fakePage{snap: browser.Snapshot{ID: "before", URL: "https://8.8.8.8/form", Text: "Remove my information", Controls: []browser.Control{{Ref: "e0", Label: "Submit"}}}}
	s.newPage = func(browser.BrowserConfig, *config.Profile, browser.URLPolicy) (pageSession, error) { return fake, nil }
	id := beginTestBatch(t, s, prepareInput{TaskIDs: []string{"broker:one"}})
	if _, err := s.openTask(ctx, openInput{id, "broker:one"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.recordResult(ctx, resultInput{id, "broker:one", "before", "submitted", "Thank you"}); err == nil {
		t.Fatal("fabricated receipt accepted")
	}
	fake.fail = true
	if _, err := s.act(ctx, actionInput{BatchID: id, TaskID: "broker:one", SnapshotID: "before", Ref: "e0", Action: "click"}); err == nil {
		t.Fatal("expected ambiguous action")
	}
	if _, err := s.observe(ctx, openInput{id, "broker:one"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.act(ctx, actionInput{BatchID: id, TaskID: "broker:one", SnapshotID: "before", Ref: "e0", Action: "click"}); err == nil {
		t.Fatal("uncertain action retried")
	}
	key := s.pages["broker:one"].lastAction
	approvedRetry(t, s, retryInput{BatchID: id, TaskID: "broker:one", ActionKey: key, Reason: "User checked the broker page; no request was submitted."})
	fake.fail = false
	if _, err := s.act(ctx, actionInput{BatchID: id, TaskID: "broker:one", SnapshotID: "before", Ref: "e0", Action: "click"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.recordResult(ctx, resultInput{id, "broker:one", "after", "submitted", "Your request has been received."}); err != nil {
		t.Fatal(err)
	}
	if !fake.closed {
		t.Fatal("completed browser was left open")
	}
	done, err := s.store.GetPendingTasks("alice", "", "completed")
	if err != nil || len(done) != 1 {
		t.Fatalf("UI completion: %+v %v", done, err)
	}
	if _, err := s.openTask(ctx, openInput{id, "broker:one"}); err == nil {
		t.Fatal("completed task reopened")
	}
}

func TestSMTPSendCooldownAndDailyCap(t *testing.T) {
	s := testService(t)
	relay := smtptest.Start(t)
	cfg, err := config.Load(s.opts.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Options.SendMode = "smtp"
	cfg.Email = relay.Email("alice@example.com")
	if err := config.Save(s.opts.ConfigPath, cfg); err != nil {
		t.Fatal(err)
	}
	id := beginTestBatch(t, s, prepareInput{BrokerIDs: []string{"one", "two"}})
	first, err := s.send(context.Background(), sendInput{id, "one"})
	if err != nil || first["sent"] != true {
		t.Fatalf("send: %+v %v", first, err)
	}
	again, err := s.send(context.Background(), sendInput{id, "one"})
	if err != nil || again["skipped"] != "cooldown" {
		t.Fatalf("cooldown: %+v %v", again, err)
	}
	second, err := s.send(context.Background(), sendInput{id, "two"})
	if err != nil || second["skipped"] != "daily limit" {
		t.Fatalf("cap: %+v %v", second, err)
	}
	if len(relay.Recipients()) != 1 {
		t.Fatalf("sent %d emails", len(relay.Recipients()))
	}
}

func TestEmailUnknownRetryAndBounceRecovery(t *testing.T) {
	s := testService(t)
	ctx := context.Background()
	relay := smtptest.Start(t)
	cfg, err := config.Load(s.opts.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Options.SendMode, cfg.Options.DailySendLimit = "smtp", 10
	cfg.Email = relay.Email("alice@example.com")
	if err := config.Save(s.opts.ConfigPath, cfg); err != nil {
		t.Fatal(err)
	}
	id := beginTestBatch(t, s, prepareInput{BrokerIDs: []string{"one"}})
	if err := s.store.ClaimAutomationAction("alice", "interrupted", "send:one"); err != nil {
		t.Fatal(err)
	}
	blocked, err := s.send(ctx, sendInput{id, "one"})
	if err != nil || blocked["sent"] != false || len(relay.Recipients()) != 0 {
		t.Fatalf("uncertain send retried: %+v, %v", blocked, err)
	}
	retry := retryInput{BatchID: id, BrokerID: "two", ActionKey: "interrupted", Reason: "User verified in the mailbox that this was not sent."}
	if _, err := s.retry(ctx, retry); err == nil {
		t.Fatal("retry escaped approved scope")
	}
	retry.BrokerID = "one"
	approvedRetry(t, s, retry)
	first, err := s.send(ctx, sendInput{id, "one"})
	if err != nil || first["sent"] != true {
		t.Fatalf("reviewed retry: %+v, %v", first, err)
	}
	if _, err := s.store.MarkFailed("alice", "one", "User marked bounced"); err != nil {
		t.Fatal(err)
	}
	second, err := s.send(ctx, sendInput{id, "one"})
	if err != nil || second["sent"] != true || len(relay.Recipients()) != 2 {
		t.Fatalf("bounce recovery: %+v, %v", second, err)
	}
}

func TestDemoIsolation(t *testing.T) {
	s, cleanup, err := NewDemo(true, os.Getenv("ERASER_TEST_CHROME"))
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	defer func() { _ = s.Close() }()
	defer func() { _ = os.RemoveAll(filepath.Dir(s.opts.ConfigPath)) }()
	if s.opts.ConfigPath == config.DefaultConfigPath() {
		t.Fatal("demo uses personal config")
	}
	tasks, err := s.listTasks(context.Background(), listInput{})
	if err != nil {
		t.Fatal(err)
	}
	if tasks["total"] != 5 {
		t.Fatalf("demo tasks: %+v", tasks)
	}
	if _, err := s.scan(context.Background(), emptyInput{}); err != nil {
		t.Fatal(err)
	}
}

func TestFailedOpenReleasesThePageAndSlot(t *testing.T) {
	s := testService(t)
	fake := &fakePage{openFail: true}
	s.newPage = func(browser.BrowserConfig, *config.Profile, browser.URLPolicy) (pageSession, error) { return fake, nil }
	id := beginTestBatch(t, s, prepareInput{TaskIDs: []string{"broker:one"}})
	out, err := s.openTask(context.Background(), openInput{BatchID: id, TaskID: "broker:one"})
	if err != nil || out["needs_user"] == nil {
		t.Fatalf("open: %v %v", out, err)
	}
	if !fake.closed || len(s.pages) != 0 {
		t.Fatalf("failed open kept the page: closed=%v pages=%d", fake.closed, len(s.pages))
	}
}
