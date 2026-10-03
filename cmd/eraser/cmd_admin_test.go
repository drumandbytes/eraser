package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/drumandbytes/eraser/internal/broker"
	"github.com/drumandbytes/eraser/internal/config"
	"github.com/drumandbytes/eraser/internal/history"
)

func lines(answers ...string) string { return strings.Join(answers, "\n") + "\n" }

func loadCfg(t *testing.T, e *cliEnv) *config.Config {
	t.Helper()
	cfg, err := config.Load(e.cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestVersion(t *testing.T) {
	out, err := runCLI(t, "", "--version")
	if err != nil {
		t.Fatal(err)
	}
	mustContain(t, out, "eraser dev", "MIT licensed")
}

func TestInitFreshManualThenUpdate(t *testing.T) {
	e := newCLIEnv(t, "")
	out, err := e.run(t, lines(
		"Jane", "", "Doe", "Jāne Doe, J. Doe", "jane@example.com", "old@example.com",
		"1 Main St", "Riga", "", "LV-1010", "Latvia", "2 Old Rd; 3 Older Rd, Jurmala", "+371 2000 0000", "",
		"2", // manual sending
		"",  // template: keep gdpr
	), "init")
	if err != nil {
		t.Fatalf("init: %v\n%s", err, out)
	}
	mustContain(t, out, "Configuration Setup", "Configuration saved", "eraser send --manual")
	cfg := loadCfg(t, e)
	p := cfg.PrimaryProfile()
	if p.FullName() != "Jane Doe" || len(p.NameVariants) != 2 || len(p.PreviousAddresses) != 2 || !cfg.IsManualSend() || cfg.Options.Template != "gdpr" {
		t.Fatalf("saved config = %+v", cfg)
	}

	// Re-running init updates: Enter keeps every value; switch to custom SMTP.
	out, err = e.run(t, lines(
		"", "", "", "", "", "", "", "", "", "", "", "", "", "",
		"1",                                                      // Eraser sends
		"8", "mail.example.org", "587", "", "", "jane", "s3cret", // custom provider
		"ccpa",
	), "init")
	if err != nil {
		t.Fatalf("init update: %v\n%s", err, out)
	}
	mustContain(t, out, "Configuration Update", "Configuration updated", "eraser send --dry-run")
	cfg = loadCfg(t, e)
	if cfg.IsManualSend() || cfg.Email.SMTP.Host != "mail.example.org" || cfg.Email.SMTP.Port != 587 ||
		cfg.Email.SMTP.Username != "jane" || cfg.Options.Template != "ccpa" || cfg.PrimaryProfile().City != "Riga" {
		t.Errorf("updated config = %+v", cfg)
	}
}

func TestInitSaveFailure(t *testing.T) {
	e := newCLIEnv(t, "")
	blocker := filepath.Join(filepath.Dir(e.cfgPath), "blocker")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	e.cfgPath = filepath.Join(blocker, "config.yaml")
	if _, err := e.run(t, lines("A", "", "B", "", "a@b.example", "", "", "", "", "", "", "", "", "", "2", ""), "init"); err == nil {
		t.Error("init under a file succeeded")
	}
}

func TestProfileLifecycle(t *testing.T) {
	e := newCLIEnv(t, manualConfig)

	out, err := e.run(t, "", "profile", "list")
	if err != nil {
		t.Fatal(err)
	}
	mustContain(t, out, "default", "spouse", "Jane Doe <jane@example.com>", "Use --profile <id>")

	// Add a third profile with its own Fastmail account.
	out, err = e.run(t, lines("Kid One!", "Kim", "", "Doe", "kim@example.com", "", "", "", "", "", "",
		"y", "3", "kim@fastmail.example", "app-pw"), "profile", "add")
	if err != nil {
		t.Fatalf("profile add: %v\n%s", err, out)
	}
	mustContain(t, out, `Added profile "kid-one"`)
	kid, err := loadCfg(t, e).GetProfile("kid-one")
	if err != nil || kid.Mail == nil || kid.Mail.Email.SMTP.Host != "smtp.fastmail.com" || kid.Mail.Inbox == nil || kid.Mail.Inbox.Server != "imap.fastmail.com" {
		t.Fatalf("kid profile = %+v, %v", kid, err)
	}

	if _, err := e.run(t, lines("spouse"), "profile", "add"); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("duplicate id: %v", err)
	}
	if _, err := e.run(t, lines(""), "profile", "add"); err == nil || !strings.Contains(err.Error(), "required") {
		t.Errorf("blank id: %v", err)
	}

	// Edit: change the city, keep the dedicated account.
	out, err = e.run(t, lines("", "", "", "", "", "Tartu", "", "", "", "", ""), "profile", "edit", "kid-one")
	if err != nil {
		t.Fatalf("profile edit: %v\n%s", err, out)
	}
	kid, _ = loadCfg(t, e).GetProfile("kid-one")
	if kid.City != "Tartu" || kid.Mail == nil {
		t.Errorf("after edit = %+v", kid)
	}
	// Edit again, dropping the dedicated account.
	if _, err := e.run(t, lines("", "", "", "", "", "", "", "", "", "", "remove"), "profile", "edit", "kid-one"); err != nil {
		t.Fatal(err)
	}
	if kid, _ = loadCfg(t, e).GetProfile("kid-one"); kid.Mail != nil {
		t.Error("mail override not removed")
	}
	if _, err := e.run(t, "", "profile", "edit", "ghost"); err == nil {
		t.Error("editing a missing profile succeeded")
	}

	out, _ = e.run(t, lines("no"), "profile", "remove", "kid-one")
	mustContain(t, out, "Cancelled.")
	if out, err := e.run(t, lines("yes"), "profile", "rm", "kid-one"); err != nil || !strings.Contains(out, "Removed profile") {
		t.Fatalf("remove: %v %s", err, out)
	}
	if _, err := e.run(t, lines("yes"), "profile", "remove", "ghost"); err == nil {
		t.Error("removing a missing profile succeeded")
	}
	if _, err := e.run(t, lines("yes"), "profile", "remove", "spouse"); err != nil {
		t.Fatal(err)
	}
	out, _ = e.run(t, "", "profile", "list")
	mustContain(t, out, "Only one profile configured")
	if _, err := e.run(t, lines("yes"), "profile", "remove", "default"); err == nil || !strings.Contains(err.Error(), "only configured profile") {
		t.Errorf("removing the last profile: %v", err)
	}
}

func TestProfileCommandsWithoutConfig(t *testing.T) {
	e := newCLIEnv(t, "")
	for _, args := range [][]string{{"profile", "list"}, {"profile", "add"}, {"profile", "edit", "x"}, {"profile", "remove", "x"}, {"status"}, {"pipeline"}} {
		if _, err := e.run(t, "", args...); err == nil || !strings.Contains(err.Error(), "config") {
			t.Errorf("%v without config: %v", args, err)
		}
	}
}

func TestListBrokers(t *testing.T) {
	e := newCLIEnv(t, "")
	out, err := e.run(t, "", "list-brokers")
	if err != nil {
		t.Fatal(err)
	}
	mustContain(t, out, "(3 total)", "Acme Data [acme]", "https://acme.example", "Opt-out: https://globex.example/optout", "(none - needs manual follow-up)", "Category: marketing")

	out, _ = e.run(t, "", "list-brokers", "--region", "us", "--missing-email")
	mustContain(t, out, "1 of 3 total match", "No Email Co")
	out, _ = e.run(t, "", "list-brokers", "--search", "GLOB", "--category", "people-search")
	mustContain(t, out, "1 of 3 total match", "Globex")

	if _, err := runCLI(t, "", "--brokers", filepath.Join(t.TempDir(), "missing.yaml"), "list-brokers"); err == nil {
		t.Error("missing broker file accepted")
	}
}

func TestAddBroker(t *testing.T) {
	// Maintaining a list file (--brokers): the entry goes into it.
	e := newCLIEnv(t, "")
	out, err := e.run(t, lines("Corner Shop OÜ", "privacy@corner.example", "https://corner.example", "", "eu", "marketing"), "add-broker")
	if err != nil {
		t.Fatalf("add-broker: %v\n%s", err, out)
	}
	mustContain(t, out, "Added Corner Shop OÜ to broker database")
	db, _ := broker.LoadFromFile(e.brokersPath)
	if db.FindByID("corner-shop-ou") == nil {
		t.Error("broker not saved to the list file")
	}
	if _, err := e.run(t, lines("Corner Shop OÜ", "x@corner.example", "", "", "eu", ""), "add-broker"); err == nil {
		t.Error("duplicate accepted")
	}
	if _, err := e.run(t, lines("", "", "", "", "", ""), "add-broker"); err == nil || !strings.Contains(err.Error(), "name is required") {
		t.Errorf("no name: %v", err)
	}
	if _, err := e.run(t, lines("Bad", "not-an-email", "", "", "mars", ""), "add-broker"); err == nil || !strings.Contains(err.Error(), "invalid broker") {
		t.Errorf("invalid: %v", err)
	}

	// --brokers pointing at a file that doesn't exist yet starts a new list.
	fresh := filepath.Join(t.TempDir(), "new.yaml")
	if _, err := runCLI(t, lines("Fresh", "privacy@fresh.example", "", "", "eu", ""), "--brokers", fresh, "add-broker"); err != nil {
		t.Fatal(err)
	}
	if db, err := broker.LoadFromFile(fresh); err != nil || len(db.Brokers) != 1 {
		t.Errorf("new list = %+v, %v", db, err)
	}

	// Installed copy (no --brokers): the user's own entries file.
	out, err = runCLI(t, lines("Tiny Broker", "privacy@tiny.example", "", "", "eu", ""), "add-broker")
	if err != nil {
		t.Fatalf("installed add-broker: %v", err)
	}
	mustContain(t, out, "to your own brokers")
	if local, _ := broker.LoadLocal(); local.FindByID("tiny-broker") == nil {
		t.Error("not saved to the local entries file")
	}
	if _, err := runCLI(t, lines("Tiny Broker", "privacy@tiny.example", "", "", "eu", ""), "add-broker"); err == nil || !strings.Contains(err.Error(), "already in the broker list") {
		t.Errorf("duplicate local: %v", err)
	}
}

func TestStatusAndPipeline(t *testing.T) {
	e := newCLIEnv(t, manualConfig)
	store, err := history.NewStore(history.DBPathFor(e.cfgPath))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	for _, r := range []history.Record{
		{ProfileID: "default", BrokerID: "acme", BrokerName: "Acme Data", Email: "privacy@acme.example", Template: "gdpr", Status: history.StatusSent, SentAt: now, SentMethod: "manual"},
		{ProfileID: "default", BrokerID: "globex", BrokerName: "Globex", Email: "dpo@globex.example", Template: "gdpr", Status: history.StatusFailed, Error: "smtp 550", SentAt: now},
	} {
		if err := store.Add(&r); err != nil {
			t.Fatal(err)
		}
	}
	_ = store.UpdatePipelineStatus("default", "acme", history.PipelineFormRequired)
	_ = store.AddBrokerResponse(&history.BrokerResponse{ProfileID: "default", BrokerID: "acme", BrokerName: "Acme Data", ResponseType: "form_required"})
	_ = store.AddPendingTask(&history.PendingTask{ProfileID: "default", BrokerID: "acme", BrokerName: "Acme Data", TaskType: history.TaskCaptcha, FormURL: "https://acme.example/form"})
	_ = store.Close()

	if _, err := e.run(t, "", "status"); err == nil || !strings.Contains(err.Error(), "multiple profiles") {
		t.Errorf("status without --profile: %v", err)
	}
	out, err := e.run(t, "", "--profile", "default", "status", "--limit", "5")
	if err != nil {
		t.Fatal(err)
	}
	mustContain(t, out, "Profile: default", "Total requests: 2", "Acme Data (gdpr, manual)", "Error: smtp 550")

	out, err = e.run(t, "", "--profile", "default", "pipeline")
	if err != nil {
		t.Fatal(err)
	}
	mustContain(t, out, "Form required:         1", "form_required: 1", "Pending:   1", "Acme Data [captcha] - https://acme.example/form")
	if _, err := e.run(t, "", "pipeline"); err == nil {
		t.Error("pipeline without --profile succeeded")
	}
}
