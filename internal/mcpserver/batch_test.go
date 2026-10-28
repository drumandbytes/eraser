package mcpserver

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/drumandbytes/eraser/internal/config"
	"github.com/drumandbytes/eraser/internal/schedule"
)

func TestPrepareValidation(t *testing.T) {
	s := testService(t)
	ctx := context.Background()
	for name, in := range map[string]prepareInput{
		"nothing selected": {},
		"too many":         {BrokerIDs: make([]string, 101)},
		"unknown broker":   {BrokerIDs: []string{"nope"}},
		"bad task id":      {TaskIDs: []string{"garbage"}},
		"missing task":     {TaskIDs: []string{"task:999"}},
	} {
		if _, err := s.prepare(ctx, in); err == nil {
			t.Errorf("%s: prepare accepted it", name)
		}
	}

	cfg, err := config.Load(s.opts.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Options.ExcludedBrokers = []string{"two"}
	if err := config.Save(s.opts.ConfigPath, cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := s.prepare(ctx, prepareInput{BrokerIDs: []string{"two"}}); err == nil || !strings.Contains(err.Error(), "excluded") {
		t.Fatalf("excluded broker: %v", err)
	}
	if _, err := s.prepare(ctx, prepareInput{BrokerIDs: []string{"one", "one"}}); err != nil {
		t.Fatalf("duplicate broker IDs should collapse: %v", err)
	}
}

func TestPrepareLimitsUnusedPreviews(t *testing.T) {
	s := testService(t)
	ctx := context.Background()
	var last error
	for range 25 {
		if _, last = s.prepare(ctx, prepareInput{BrokerIDs: []string{"one"}}); last != nil {
			break
		}
	}
	if last == nil || !strings.Contains(last.Error(), "too many unused previews") {
		t.Fatalf("no cap on unused previews: %v", last)
	}
}

func TestBeginRefusesUnknownAndSecondBatch(t *testing.T) {
	s := testService(t)
	ctx := context.Background()
	if _, err := s.begin(ctx, beginInput{PlanID: "unknown"}); err == nil {
		t.Fatal("began an unknown plan")
	}
	id := beginTestBatch(t, s, prepareInput{BrokerIDs: []string{"one"}})
	if id == "" {
		t.Fatal("no batch ID")
	}
	preview, err := s.prepare(ctx, prepareInput{BrokerIDs: []string{"two"}})
	if err != nil {
		t.Fatal(err)
	}
	approveTestPlan(t, s, preview["plan_id"].(string))
	if _, err := s.begin(ctx, beginInput{PlanID: preview["plan_id"].(string)}); err == nil {
		t.Fatal("began a second batch while one was active")
	}
}

func TestSendGuardRails(t *testing.T) {
	s := testService(t)
	ctx := context.Background()
	setMode := func(mode string, dryRun bool) {
		t.Helper()
		cfg, err := config.Load(s.opts.ConfigPath)
		if err != nil {
			t.Fatal(err)
		}
		cfg.Options.SendMode, cfg.Options.DryRun = mode, dryRun
		if err := config.Save(s.opts.ConfigPath, cfg); err != nil {
			t.Fatal(err)
		}
		s.endBatch()
	}

	id := beginTestBatch(t, s, prepareInput{BrokerIDs: []string{"one"}})
	if _, err := s.send(ctx, sendInput{BatchID: id, BrokerID: "two"}); err == nil || !strings.Contains(err.Error(), "outside") {
		t.Fatalf("broker outside the batch: %v", err)
	}
	if _, err := s.send(ctx, sendInput{BatchID: id, BrokerID: "one"}); err == nil || !strings.Contains(err.Error(), "manual") {
		t.Fatalf("manual mode: %v", err)
	}

	setMode("auto", true)
	id = beginTestBatch(t, s, prepareInput{BrokerIDs: []string{"one"}})
	out, err := s.send(ctx, sendInput{BatchID: id, BrokerID: "one"})
	if err != nil || out["dry_run"] != true || out["sent"] != false {
		t.Fatalf("dry run: %v %v", out, err)
	}

	setMode("auto", false)
	id = beginTestBatch(t, s, prepareInput{BrokerIDs: []string{"one"}})
	release, ok, err := schedule.TryLock(filepath.Dir(s.opts.ConfigPath))
	if err != nil || !ok {
		t.Fatalf("could not take the send lock: %v", err)
	}
	defer release()
	if _, err := s.send(ctx, sendInput{BatchID: id, BrokerID: "one"}); err == nil || !strings.Contains(err.Error(), "another send cycle") {
		t.Fatalf("busy lock: %v", err)
	}
}

func TestToolsRefuseWithoutApprovedBatchOrConfig(t *testing.T) {
	s := testService(t)
	ctx := context.Background()
	none := openInput{BatchID: "nobatch", TaskID: "task:1"}
	for name, call := range map[string]func() error{
		"open_task":      func() error { _, err := s.openTask(ctx, none); return err },
		"observe_page":   func() error { _, err := s.observe(ctx, none); return err },
		"get_screenshot": func() error { _, err := s.screenshot(ctx, none); return err },
		"browser_action": func() error {
			_, err := s.act(ctx, actionInput{BatchID: "nobatch", TaskID: "task:1", Action: "click"})
			return err
		},
		"record_result": func() error {
			_, err := s.recordResult(ctx, resultInput{BatchID: "nobatch", TaskID: "task:1", Outcome: "progress"})
			return err
		},
		"retry_action": func() error {
			_, err := s.retry(ctx, retryInput{BatchID: "nobatch", TaskID: "task:1", ActionKey: "k", Reason: "r"})
			return err
		},
	} {
		if err := call(); err == nil {
			t.Errorf("%s worked without an approved batch", name)
		}
	}

	if err := os.Remove(s.opts.ConfigPath); err != nil {
		t.Fatal(err)
	}
	for name, call := range map[string]func() error{
		"status":       func() error { _, err := s.status(ctx, emptyInput{}); return err },
		"list_brokers": func() error { _, err := s.listBrokers(ctx, listInput{}); return err },
		"list_tasks":   func() error { _, err := s.listTasks(ctx, listInput{}); return err },
		"get_task":     func() error { _, err := s.getTask(ctx, taskInput{TaskID: "task:1"}); return err },
		"scan":         func() error { _, err := s.scan(ctx, emptyInput{}); return err },
		"prepare":      func() error { _, err := s.prepare(ctx, prepareInput{BrokerIDs: []string{"one"}}); return err },
	} {
		if err := call(); err == nil {
			t.Errorf("%s worked without a config", name)
		}
	}
}

func TestCustomTemplateInPrepareAndSend(t *testing.T) {
	s := testService(t)
	ctx := context.Background()
	edit := func(mutate func(*config.Config)) {
		t.Helper()
		cfg, err := config.Load(s.opts.ConfigPath)
		if err != nil {
			t.Fatal(err)
		}
		mutate(cfg)
		if err := config.Save(s.opts.ConfigPath, cfg); err != nil {
			t.Fatal(err)
		}
	}

	edit(func(c *config.Config) { c.Options.Template, c.Options.CustomTemplate = "custom", "Dear {{.Nope" })
	if _, err := s.prepare(ctx, prepareInput{BrokerIDs: []string{"one"}}); err == nil || !strings.Contains(err.Error(), "invalid template") {
		t.Fatalf("prepare with a broken custom template: %v", err)
	}

	edit(func(c *config.Config) { c.Options.CustomTemplate = "Dear {{.BrokerName}}" })
	if _, err := s.prepare(ctx, prepareInput{BrokerIDs: []string{"one"}}); err != nil {
		t.Fatalf("prepare with a custom template: %v", err)
	}
}
