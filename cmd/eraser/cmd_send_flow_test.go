package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/drumandbytes/eraser/internal/history"
	"github.com/drumandbytes/eraser/internal/smtptest"
)

func stats(t *testing.T, e *cliEnv, profile string) (total, sent, failed int) {
	t.Helper()
	store, err := history.NewStore(history.DBPathFor(e.cfgPath))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	total, sent, failed, _ = store.GetStats(profile)
	return
}

func TestSendDryRunAndReal(t *testing.T) {
	rl := smtptest.Start(t)
	e := newCLIEnv(t, relayConfig(rl, ""))

	out, err := e.run(t, "", "send", "--dry-run")
	if err != nil {
		t.Fatal(err)
	}
	mustContain(t, out, "DRY RUN MODE", "Would send: GDPR Data Erasure Request", "No email on file - see notes", "Dry run complete: 2 brokers")
	if len(rl.Recipients()) != 0 {
		t.Fatal("dry run sent mail")
	}

	rl.Reject("dpo@globex.example", true)
	out, err = e.run(t, "", "send")
	if err != nil {
		t.Fatal(err)
	}
	mustContain(t, out, "Sent successfully", "Failed:", "Complete: 1 sent, 1 failed")
	if got := rl.Recipients(); len(got) != 1 || got[0] != "privacy@acme.example" {
		t.Errorf("relay got %v", got)
	}

	// Only the failed broker is retried.
	rl.Reject("dpo@globex.example", false)
	out, _ = e.run(t, "", "send", "--status", "failed")
	mustContain(t, out, "Processing 1 brokers", "Complete: 1 sent, 0 failed")
	if _, sent, failed := stats(t, e, "default"); sent != 2 || failed != 1 {
		t.Errorf("history sent/failed = %d/%d", sent, failed)
	}

	out, _ = e.run(t, "", "send", "--broker", "acme")
	mustContain(t, out, `Nothing to send - no brokers match status "eligible"`)
	out, _ = e.run(t, "", "send", "--broker", "acme", "--resend")
	mustContain(t, out, "Complete: 1 sent")
}

func TestSendFiltersAndRefusals(t *testing.T) {
	rl := smtptest.Start(t)
	e := newCLIEnv(t, relayConfig(rl, ""))
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"send", "--status", "sometimes"}, "invalid status"},
		{[]string{"send", "--list", "partial"}, "invalid broker list"},
		{[]string{"send", "--broker", "ghost"}, "unknown broker ID"},
	}
	for _, c := range cases {
		if _, err := e.run(t, "", c.args...); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%v: %v, want %q", c.args, err, c.want)
		}
	}
	out, err := e.run(t, "", "send", "--region", "global")
	if err != nil || !strings.Contains(out, "No brokers to process.") {
		t.Errorf("empty selection: %v %s", err, out)
	}
	out, _ = e.run(t, "", "send", "--dry-run", "--region", "eu", "--exclude", "acme")
	mustContain(t, out, "No brokers to process.")

	noEmail := newCLIEnv(t, "profiles:\n  - id: default\n    first_name: A\n    last_name: B\n    email: a@b.example\n")
	if _, err := noEmail.run(t, "", "send"); err == nil || !strings.Contains(err.Error(), "invalid config") {
		t.Errorf("send without email config: %v", err)
	}
	if len(rl.Recipients()) != 0 {
		t.Error("a refused send reached the relay")
	}
}

func TestSendDailyLimit(t *testing.T) {
	rl := smtptest.Start(t)
	e := newCLIEnv(t, relayConfig(rl, "  daily_send_limit: 1\n"))
	out, _ := e.run(t, "", "send")
	mustContain(t, out, "sending 1 of 2 remaining brokers", "Complete: 1 sent")
	out, _ = e.run(t, "", "send")
	mustContain(t, out, "Daily send limit reached (1/1")
	out, _ = e.run(t, "", "send", "--ignore-daily-limit")
	mustContain(t, out, "Complete: 1 sent")
	if len(rl.Recipients()) != 2 {
		t.Errorf("relay got %v", rl.Recipients())
	}
}

func TestSendStopsOnRepeatedAuthFailures(t *testing.T) {
	rl := smtptest.Start(t)
	rl.FailAuth()
	e := newCLIEnv(t, relayConfig(rl, ""))
	var list strings.Builder
	list.WriteString("brokers:\n")
	for _, id := range []string{"a1", "a2", "a3", "a4"} {
		list.WriteString("  - {id: " + id + ", name: " + id + ", email: privacy@" + id + ".example, region: eu}\n")
	}
	if err := os.WriteFile(e.brokersPath, []byte(list.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := e.run(t, "", "send")
	if err == nil || !strings.Contains(err.Error(), "3 consecutive authentication failures") {
		t.Fatalf("err = %v", err)
	}
	if total, _, _ := stats(t, e, "default"); total != 3 {
		t.Errorf("attempts = %d, want the 4th broker untouched", total)
	}
}

func TestSendManual(t *testing.T) {
	e := newCLIEnv(t, manualConfig)
	out, err := e.run(t, lines("s", "n", "y"), "--profile", "default", "send")
	if err != nil {
		t.Fatal(err)
	}
	mustContain(t, out, "Manual mode", "From: jane@example.com", "To: privacy@acme.example", "No email on file and no opt-out URL", "Recorded 2 as sent, 1 left for later")
	if _, sent, _ := stats(t, e, "default"); sent != 2 {
		t.Errorf("recorded = %d", sent)
	}

	out, _ = e.run(t, lines("q"), "--profile", "default", "send", "--resend")
	mustContain(t, out, "Recorded 0 as sent, 0 left")
	out, _ = e.run(t, lines("n", "n", "q"), "--profile", "spouse", "send", "--broker", "globex,noemail,acme")
	mustContain(t, out, "Recorded 0 as sent, 2 left")
	// --manual on a sending config never touches SMTP either.
	rl := smtptest.Start(t)
	smtp := newCLIEnv(t, relayConfig(rl, ""))
	out, _ = smtp.run(t, lines("s", "s", "n"), "send", "--manual")
	mustContain(t, out, "Recorded 2 as sent")
	if len(rl.Recipients()) != 0 {
		t.Error("manual mode sent mail")
	}
}

func TestDraft(t *testing.T) {
	e := newCLIEnv(t, manualConfig)
	out, err := e.run(t, "", "--profile", "default", "draft", "acme")
	if err != nil {
		t.Fatal(err)
	}
	mustContain(t, out, "To: privacy@acme.example", "Subject: GDPR Data Erasure Request", "eraser mark-sent acme")

	out, _ = e.run(t, "", "--profile", "default", "draft", "noemail")
	mustContain(t, out, "No Email Co has no email on file")

	dir := filepath.Join(t.TempDir(), "drafts")
	out, err = e.run(t, "", "--profile", "default", "draft", "-o", dir)
	if err != nil {
		t.Fatal(err)
	}
	mustContain(t, out, "2 .eml file(s)", "1 broker(s) skipped")
	eml, err := os.ReadFile(filepath.Join(dir, "acme.eml"))
	if err != nil || !strings.Contains(string(eml), "To: privacy@acme.example") || !strings.Contains(string(eml), "From: jane@example.com") {
		t.Errorf("acme.eml = %s, %v", eml, err)
	}

	if _, err := e.run(t, "", "--profile", "default", "draft"); err == nil || !strings.Contains(err.Error(), "only works for one broker") {
		t.Errorf("several brokers to the terminal: %v", err)
	}
	if _, err := e.run(t, "", "--profile", "default", "draft", "ghost"); err == nil {
		t.Error("unknown broker accepted")
	}
	out, _ = e.run(t, "", "--profile", "default", "draft", "--region", "global")
	mustContain(t, out, "No brokers match.")
	blocker := filepath.Join(t.TempDir(), "file")
	_ = os.WriteFile(blocker, nil, 0o600)
	if _, err := e.run(t, "", "--profile", "default", "draft", "-o", filepath.Join(blocker, "x")); err == nil {
		t.Error("draft into a path under a file succeeded")
	}
}

func TestMarkSent(t *testing.T) {
	e := newCLIEnv(t, manualConfig)
	if _, err := e.run(t, "", "mark-sent"); err == nil || !strings.Contains(err.Error(), "give one or more broker ids") {
		t.Errorf("no selection: %v", err)
	}
	out, err := e.run(t, "", "--profile", "spouse", "mark-sent", "--region", "us", "--dry-run")
	if err != nil {
		t.Fatal(err)
	}
	mustContain(t, out, "Would record 2 broker(s)", "Globex (globex)")
	if total, _, _ := stats(t, e, "spouse"); total != 0 {
		t.Fatal("dry run wrote history")
	}
	out, err = e.run(t, "", "--profile", "spouse", "mark-sent", "acme", "globex")
	if err != nil {
		t.Fatal(err)
	}
	mustContain(t, out, "Profile: spouse", "2 broker(s).")
	if _, sent, _ := stats(t, e, "spouse"); sent != 2 {
		t.Errorf("spouse sent = %d", sent)
	}
	out, _ = e.run(t, "", "--profile", "spouse", "mark-sent", "--category", "nope")
	mustContain(t, out, "No brokers match.")
	if _, err := e.run(t, "", "--profile", "spouse", "mark-sent", "ghost"); err == nil {
		t.Error("unknown broker accepted")
	}
}

func TestExportCommand(t *testing.T) {
	e := newCLIEnv(t, manualConfig)
	if _, err := e.run(t, "", "--profile", "default", "mark-sent", "acme"); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "ev.json")
	if _, err := e.run(t, "", "--profile", "default", "export", "--format", "json", "-o", out, "--since", "2020-01-01"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(out)
	if err != nil || !strings.Contains(string(data), `"broker_id": "acme"`) {
		t.Errorf("export = %s, %v", data, err)
	}
	if _, err := e.run(t, "", "--profile", "default", "export", "--since", "last week", "-o", out); err == nil {
		t.Error("bad --since accepted")
	}
	if _, err := e.run(t, "", "export", "-o", out); err == nil {
		t.Error("export without --profile on a two-profile config succeeded")
	}
}
