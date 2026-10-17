package mcpserver

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/drumandbytes/eraser/internal/config"
	"github.com/drumandbytes/eraser/internal/history"
	"github.com/drumandbytes/eraser/internal/imaptest"
)

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func TestResolveAndGetTask(t *testing.T) {
	s := testService(t)
	ctx := context.Background()
	_, p, db, err := s.load()
	if err != nil {
		t.Fatal(err)
	}

	task := &history.PendingTask{ProfileID: "alice", BrokerID: "one", BrokerName: "One", TaskType: history.TaskManualForm, FormURL: "https://8.8.8.8/form", Notes: "note"}
	if err := s.store.AddPendingTask(task); err != nil {
		t.Fatal(err)
	}
	long := strings.Repeat("x", 17000)
	reply := &history.BrokerResponse{ProfileID: "alice", BrokerID: "one", BrokerName: "One", ResponseType: "form_required", FormURL: "https://8.8.8.8/other", EmailFrom: "privacy@one.example", EmailSubject: "Use the form", EmailBody: long, ReceivedAt: time.Now()}
	if err := s.store.AddBrokerResponse(reply); err != nil {
		t.Fatal(err)
	}

	for id, want := range map[string]string{"broker:one": "form", "task:" + itoa(task.ID): "form", "response:" + itoa(reply.ID): "form"} {
		got, err := s.resolveTask(p, db, id)
		if err != nil || got.Kind != want {
			t.Fatalf("resolve %s: %+v %v", id, got, err)
		}
	}
	for _, bad := range []string{"nocolon", "broker:missing", "task:0", "task:abc", "task:999", "response:999", "other:1"} {
		if _, err := s.resolveTask(p, db, bad); err == nil {
			t.Fatalf("resolved bad task ID %q", bad)
		}
	}

	out, err := s.getTask(ctx, taskInput{TaskID: "response:" + itoa(reply.ID)})
	if err != nil {
		t.Fatal(err)
	}
	r, ok := out["reply"].(map[string]any)
	if !ok || r["truncated"] != true || len(r["body"].(string)) != 16000 || r["untrusted"] != true {
		t.Fatalf("reply=%v", out["reply"])
	}
	if out, err = s.getTask(ctx, taskInput{TaskID: "task:" + itoa(task.ID)}); err != nil || out["reply"] != nil {
		t.Fatalf("task reply=%v err=%v", out["reply"], err)
	}
	if _, err := s.getTask(ctx, taskInput{TaskID: "nope"}); err == nil {
		t.Fatal("getTask accepted an invalid ID")
	}
}

func TestListTasksBrokersStatusAndScanGuards(t *testing.T) {
	s := testService(t)
	ctx := context.Background()
	if err := s.store.AddPendingTask(&history.PendingTask{ProfileID: "alice", BrokerID: "one", BrokerName: "One", TaskType: history.TaskReview, FormURL: "https://8.8.8.8/form"}); err != nil {
		t.Fatal(err)
	}
	if err := s.store.AddBrokerResponse(&history.BrokerResponse{ProfileID: "alice", BrokerID: "two", BrokerName: "Two", ResponseType: "confirmation_required", ConfirmURL: "https://8.8.8.8/confirm", NeedsReview: true, ReceivedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	tasks, err := s.listTasks(ctx, listInput{})
	if err != nil || tasks["total"].(int) != 2 {
		t.Fatalf("tasks=%v err=%v", tasks, err)
	}
	page, err := s.listTasks(ctx, listInput{Offset: 1, Limit: 1})
	if err != nil || len(page["tasks"].([]Task)) != 1 {
		t.Fatalf("page=%v err=%v", page, err)
	}
	brokers, err := s.listBrokers(ctx, listInput{Limit: 1})
	if err != nil || brokers["total"].(int) != 2 || brokers["next_offset"].(int) != 1 {
		t.Fatalf("brokers=%v err=%v", brokers, err)
	}
	status, err := s.status(ctx, emptyInput{})
	if err != nil || status["profile_id"] != "alice" || status["batch_active"] != false {
		t.Fatalf("status=%v err=%v", status, err)
	}
	if _, err := s.scan(ctx, emptyInput{}); err == nil || !strings.Contains(err.Error(), "Inbox Monitoring") {
		t.Fatalf("scan without inbox: %v", err)
	}
	if _, err := s.send(ctx, sendInput{BatchID: "nobatch"}); err == nil {
		t.Fatal("send without an approved batch")
	}
}

func TestScanRecordsOnlyThisProfilesReplies(t *testing.T) {
	s := testService(t)
	srv := imaptest.Start(t, false)
	cfg, err := config.Load(s.opts.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Inbox = srv.Inbox
	if err := config.Save(s.opts.ConfigPath, cfg); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	for _, r := range []history.Record{
		{ProfileID: "alice", BrokerID: "one", BrokerName: "One", Email: "privacy@one.example", Status: history.StatusSent, SentAt: now},
		{ProfileID: "bob", BrokerID: "two", BrokerName: "Two", Email: "privacy@two.example", Status: history.StatusSent, SentAt: now},
	} {
		if err := s.store.Add(&r); err != nil {
			t.Fatal(err)
		}
	}
	srv.Deliver(t, "INBOX", "privacy@one.example", "Re: request", "We have deleted your personal data.")
	srv.Deliver(t, "INBOX", "privacy@two.example", "Re: request", "We have deleted your personal data.")

	out, err := s.scan(context.Background(), emptyInput{})
	if err != nil || out["new_replies"] != 1 {
		t.Fatalf("scan=%v err=%v", out, err)
	}
	out, err = s.scan(context.Background(), emptyInput{})
	if err != nil || out["new_replies"] != 0 {
		t.Fatalf("rescan=%v err=%v", out, err)
	}
}
