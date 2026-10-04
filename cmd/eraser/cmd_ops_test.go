package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/drumandbytes/eraser/internal/broker"
	"github.com/drumandbytes/eraser/internal/history"
	"github.com/drumandbytes/eraser/internal/imaptest"
	"github.com/drumandbytes/eraser/internal/schedule"
	"github.com/drumandbytes/eraser/internal/smtptest"
)

// imapInbox serves INBOX with the given (from, subject, body) messages and
// returns its inbox: YAML.
func imapInbox(t *testing.T, mails ...[3]string) string {
	t.Helper()
	srv := imaptest.Start(t, false)
	for _, m := range mails {
		srv.Deliver(t, "INBOX", m[0], m[1], m[2])
	}
	return "inbox:\n  enabled: true\n  server: 127.0.0.1\n  port: " + strconv.Itoa(srv.Inbox.Port) + "\n  email: username\n  password: password\n"
}

func addSent(t *testing.T, e *cliEnv, profile, brokerID, email string) {
	t.Helper()
	store, err := history.NewStore(history.DBPathFor(e.cfgPath))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	if err := store.Add(&history.Record{ProfileID: profile, BrokerID: brokerID, BrokerName: brokerID, Email: email,
		Template: "gdpr", Status: history.StatusSent, SentAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
}

func TestMonitor(t *testing.T) {
	rl := smtptest.Start(t)
	inboxYAML := imapInbox(t,
		[3]string{"privacy@acme.example", "Re: Erasure request", "We have deleted your personal data from our systems."},
		[3]string{"dpo@globex.example", "Your request", "Please complete our opt-out form at https://globex.example/optout"},
	)
	e := newCLIEnv(t, relayConfig(rl, inboxYAML))
	addSent(t, e, "default", "acme", "privacy@acme.example")

	out, err := e.run(t, "", "monitor", "--days", "3")
	if err != nil {
		t.Fatalf("monitor: %v\n%s", err, out)
	}
	mustContain(t, out, "Found 2 emails from data brokers", "2 new", "Acme Data - success", "Globex - form_required", "Form URL:", "Success:          1")

	out, _ = e.run(t, "", "monitor")
	mustContain(t, out, "Found 2 emails from data brokers", "0 new")

	// The deprecated --once still works.
	if _, err := e.run(t, "", "monitor", "--once"); err != nil {
		t.Errorf("--once: %v", err)
	}
}

func TestMonitorNotConfiguredAndUnreachable(t *testing.T) {
	e := newCLIEnv(t, manualConfig)
	out, err := e.run(t, "", "monitor")
	if err == nil || !strings.Contains(out, "Inbox monitoring is not configured") {
		t.Errorf("unconfigured: %v %s", err, out)
	}
	down := newCLIEnv(t, manualConfig+"inbox:\n  enabled: true\n  server: 127.0.0.1\n  port: 1\n  email: a@b.example\n  password: p\n")
	if _, err := down.run(t, "", "monitor"); err == nil || !strings.Contains(err.Error(), "failed to connect") {
		t.Errorf("unreachable: %v", err)
	}
}

func TestCleanupBounces(t *testing.T) {
	ndr := func(addr string) [3]string {
		return [3]string{"Mail Delivery Subsystem <mailer-daemon@mx.example>", "Delivery Status Notification (Failure)",
			"Delivery to the following recipient failed permanently: " + addr}
	}
	inboxYAML := imapInbox(t, ndr("privacy@acme.example"), ndr("dpo@globex.example"), ndr("nobody@unknown.example"),
		[3]string{"postmaster@mx.example", "Undeliverable", "sorry"})
	e := newCLIEnv(t, manualConfig+inboxYAML)
	addSent(t, e, "spouse", "acme", "privacy@acme.example") // globex was never emailed: a spoofed bounce

	out, err := e.run(t, "", "cleanup-bounces")
	if err != nil {
		t.Fatalf("dry run: %v\n%s", err, out)
	}
	mustContain(t, out, "Found 4 bounced email(s)", "❌ privacy@acme.example", "no record of ever emailing them",
		"nobody@unknown.example - not found", "Could not extract", "Found 1 broker(s) with invalid email addresses")

	out, err = e.run(t, "", "cleanup-bounces", "--remove")
	if err != nil {
		t.Fatal(err)
	}
	mustContain(t, out, "Cleared Acme Data (privacy@acme.example)", "Cleared 1 broker email address(es)")
	db, _ := broker.LoadFromFile(e.brokersPath)
	if acme := db.FindByID("acme"); acme.Email != "" || !strings.Contains(acme.Notes, "undeliverable") {
		t.Errorf("acme after cleanup = %+v", acme)
	}
	if _, err := os.Stat(e.brokersPath + ".bak"); err != nil {
		t.Error("no backup written")
	}

	// Installed copy (no --brokers): the correction goes to the user's own entries.
	out, err = runCLI(t, "", "--config", e.cfgPath, "cleanup-bounces")
	if err != nil {
		t.Fatal(err)
	}
	mustContain(t, out, "not found in broker database") // acme.example isn't in the shipped list

	noInbox := newCLIEnv(t, manualConfig)
	if _, err := noInbox.run(t, "", "cleanup-bounces"); err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Errorf("no inbox: %v", err)
	}
}

func TestCleanupBouncesNoneFound(t *testing.T) {
	e := newCLIEnv(t, manualConfig+imapInbox(t))
	out, err := e.run(t, "", "cleanup-bounces")
	if err != nil {
		t.Fatal(err)
	}
	mustContain(t, out, "No bounced emails found")
}

func TestMarkBounced(t *testing.T) {
	e := newCLIEnv(t, manualConfig)
	addSent(t, e, "default", "acme", "privacy@acme.example")
	out, err := e.run(t, "", "--profile", "default", "mark-bounced", "acme", "globex", "--note", "550 user unknown")
	if err != nil {
		t.Fatal(err)
	}
	mustContain(t, out, "acme - marked failed", `globex - no "sent" record`, "Updated 1 broker(s), 1 skipped.")
	out, _ = e.run(t, "", "--profile", "default", "mark-bounced", "acme")
	mustContain(t, out, "Updated 0 broker(s), 1 skipped.")
	addSent(t, e, "default", "globex", "dpo@globex.example")
	out, _ = e.run(t, "", "--profile", "default", "mark-bounced", "globex")
	mustContain(t, out, "Updated 1 broker(s).")
	if _, err := e.run(t, "", "mark-bounced", "acme"); err == nil {
		t.Error("ambiguous profile accepted")
	}
}

func TestConfirm(t *testing.T) {
	hits := 0
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if r.URL.Path == "/hop" {
			http.Redirect(w, r, "/ok", http.StatusFound)
			return
		}
		if r.URL.Path == "/ok" {
			_, _ = w.Write([]byte("<html>Your opt-out request has been confirmed.</html>"))
			return
		}
		http.Error(w, "invalid or expired token", http.StatusBadRequest)
	}))
	defer site.Close()

	e := newCLIEnv(t, manualConfig)
	store, _ := history.NewStore(history.DBPathFor(e.cfgPath))
	_ = store.AddBrokerResponse(&history.BrokerResponse{ProfileID: "default", BrokerID: "acme", BrokerName: "Acme Data",
		ResponseType: "confirmation_required", ConfirmURL: site.URL + "/ok"})
	_ = store.AddBrokerResponse(&history.BrokerResponse{ProfileID: "default", BrokerID: "noemail", BrokerName: "No Email Co",
		ResponseType: "confirmation_required", ConfirmURL: site.URL + "/hop"})
	_ = store.AddBrokerResponse(&history.BrokerResponse{ProfileID: "default", BrokerID: "globex", BrokerName: "Globex",
		ResponseType: "confirmation_required", ConfirmURL: site.URL + "/expired"})
	_ = store.Close()

	out, err := e.run(t, "", "--profile", "default", "confirm", "--pending", "--dry-run")
	if err != nil {
		t.Fatal(err)
	}
	// 127.0.0.1 is no broker's domain: refused unless validation is off.
	mustContain(t, out, "links to process: 3", "is not a known broker domain", "Dry run complete")
	if hits != 0 {
		t.Fatal("dry run / refused links were fetched")
	}

	out, err = e.run(t, "", "--profile", "default", "confirm", "--pending", "--validate-domain=false")
	if err != nil {
		t.Fatal(err)
	}
	// Redirect hops are always re-checked against broker domains, even with
	// --validate-domain=false, so a token can't be carried off-site.
	mustContain(t, out, "redirect to disallowed domain", "Link expired", "Complete: 1 confirmed, 2 failed")

	out, _ = e.run(t, "", "--profile", "default", "confirm", "--broker", "acme", "--dry-run", "--validate-domain=false")
	mustContain(t, out, "Would click this link")
	if _, err := e.run(t, "", "--profile", "default", "confirm", "--broker", "ghost"); err == nil {
		t.Error("unknown broker accepted")
	}
	out, _ = e.run(t, "", "--profile", "default", "confirm", "--url", "http://[::1", "--validate-domain=true")
	mustContain(t, out, "Invalid URL")
	out, _ = e.run(t, "", "--profile", "default", "confirm", "--url", "http://127.0.0.1:1/x", "--validate-domain=false")
	mustContain(t, out, "Error:")
	if _, err := e.run(t, "", "--profile", "default", "confirm"); err == nil {
		t.Error("confirm without a target succeeded")
	}
	out, _ = e.run(t, "", "--profile", "spouse", "confirm", "--pending")
	mustContain(t, out, "No pending confirmation links")
}

func TestFillSelectionWithoutBrowser(t *testing.T) {
	e := newCLIEnv(t, manualConfig)
	if _, err := e.run(t, "", "--profile", "default", "fill"); err == nil || !strings.Contains(err.Error(), "--url, --broker, or --pending") {
		t.Errorf("no target: %v", err)
	}
	if _, err := e.run(t, "", "--profile", "default", "fill", "--broker", "acme"); err == nil {
		t.Error("broker without a form URL accepted")
	}
	out, _ := e.run(t, "", "--profile", "default", "fill", "--pending")
	mustContain(t, out, "No pending forms to fill")

	// A URL outside the broker allowlist is refused before Chrome starts.
	out, err := e.run(t, "", "--profile", "default", "fill", "--url", "https://evil.example/optout", "--wait", "--screenshots", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	mustContain(t, out, "--wait requires --headless=false", "not in known broker list", "Processed 1 forms")

	store, _ := history.NewStore(history.DBPathFor(e.cfgPath))
	_ = store.AddBrokerResponse(&history.BrokerResponse{ProfileID: "default", BrokerID: "acme", BrokerName: "Acme Data",
		ResponseType: "form_required", FormURL: "https://evil.example/form"})
	_ = store.Close()
	out, _ = e.run(t, "", "--profile", "default", "fill", "--pending")
	mustContain(t, out, "Forms to process: 1", "Broker: acme")
	out, _ = e.run(t, "", "--profile", "default", "fill", "--broker", "acme")
	mustContain(t, out, "Processed 1 forms")
}

func TestAuditCommand(t *testing.T) {
	orig := auditCheckerFor
	t.Cleanup(func() { auditCheckerFor = orig })
	auditCheckerFor = func(time.Duration) *auditChecker { return stubChecker(0, 0, 0, fmt.Errorf("refused")) }

	e := newCLIEnv(t, "")
	out, err := e.run(t, "", "audit-brokers", "--region", "eu")
	if err != nil {
		t.Fatal(err)
	}
	mustContain(t, out, "Auditing 1 broker(s)", "acme")
	if _, err := e.run(t, "", "audit-brokers", "--fail-on-dead"); err == nil || !strings.Contains(err.Error(), "dead email domain") {
		t.Errorf("--fail-on-dead: %v", err)
	}
	out, err = e.run(t, "", "audit-brokers", "--fix")
	if err != nil {
		t.Fatal(err)
	}
	mustContain(t, out, "Cleared 2 dead email address(es)")
	db, _ := broker.LoadFromFile(e.brokersPath)
	if db.FindByID("acme").Email != "" {
		t.Error("--fix didn't clear the dead address")
	}
	out, _ = e.run(t, "", "audit-brokers", "--fix")
	mustContain(t, out, "Nothing to fix")

	auditCheckerFor = func(time.Duration) *auditChecker { return stubChecker(1, 1, 200, nil) }
	if _, err := e.run(t, "", "audit-brokers", "--fail-on-dead"); err != nil {
		t.Errorf("all alive: %v", err)
	}
	if _, err := runCLI(t, "", "--brokers", filepath.Join(t.TempDir(), "nope.yaml"), "audit-brokers", "--fix"); err == nil {
		t.Error("--fix without a broker file succeeded")
	}
}

// The real checker falls back to GET when a site drops HEAD.
func TestAuditCheckerHeadFallsBackToGet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			conn, _, _ := w.(http.Hijacker).Hijack()
			_ = conn.Close()
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	resp, err := newAuditChecker(5 * time.Second).httpHead(srv.URL)
	if err != nil || resp.StatusCode != http.StatusOK || resp.Request.Method != http.MethodGet {
		t.Fatalf("resp = %+v, %v", resp, err)
	}
	_ = resp.Body.Close()
	if _, err := newAuditChecker(time.Second).httpHead("http://127.0.0.1:1"); err == nil {
		t.Error("unreachable site: no error")
	}
	if _, err := newAuditChecker(time.Second).httpHead("://bad"); err == nil {
		t.Error("bad URL: no error")
	}
}

func TestValidateBrokers(t *testing.T) {
	out, err := runCLI(t, "", "validate-brokers")
	if err != nil {
		t.Fatal(err)
	}
	mustContain(t, out, "embedded broker list is valid", "embedded verified list is valid")

	e := newCLIEnv(t, "")
	if _, err := runCLI(t, "", "validate-brokers", e.brokersPath); err == nil {
		t.Error("a 3-entry list passed the size floor")
	}
	if _, err := runCLI(t, "", "validate-brokers", filepath.Join(t.TempDir(), "missing.yaml")); err == nil {
		t.Error("missing file accepted")
	}
	var list strings.Builder
	list.WriteString("brokers:\n")
	for i := 0; i < broker.MinVerifiedBrokerCount; i++ {
		fmt.Fprintf(&list, "  - {id: b%d, name: B%d, email: b%d@example.com, region: eu}\n", i, i, i)
	}
	path := filepath.Join(t.TempDir(), "ok.yaml")
	_ = os.WriteFile(path, []byte(list.String()), 0o600)
	out, err = runCLI(t, "", "--brokers", path, "validate-brokers")
	if err != nil || !strings.Contains(out, "is valid ("+strconv.Itoa(broker.MinVerifiedBrokerCount)+" brokers)") {
		t.Errorf("valid file: %v %s", err, out)
	}
}

func TestGuidesHTML(t *testing.T) {
	e := newCLIEnv(t, "")
	dir := t.TempDir()
	out, err := e.run(t, "", "guides", "--format", "html", "-o", dir)
	if err != nil {
		t.Fatal(err)
	}
	mustContain(t, out, "Guides generated")
	matches, _ := filepath.Glob(filepath.Join(dir, "*", "*.html"))
	if len(matches) == 0 {
		t.Fatalf("no html pages under %s", dir)
	}
	if _, err := e.run(t, "", "guides", "--format", "pdf", "-o", dir); err == nil {
		t.Error("unknown format accepted")
	}
}

func TestScheduleStatusAndInstall(t *testing.T) {
	e := newCLIEnv(t, manualConfig)
	out, err := e.run(t, "", "schedule", "status")
	if err != nil {
		t.Fatal(err)
	}
	mustContain(t, out, "Last cycle:   none yet")
	if schedule.Supported() {
		mustContain(t, out, "not installed")
	}

	_ = schedule.SaveState(filepath.Dir(e.cfgPath), schedule.State{LastRun: time.Now().Add(-time.Hour), Mode: "custom", Sent: 3, Error: "inbox down"})
	out, _ = e.run(t, "", "schedule", "status")
	mustContain(t, out, "via custom), 3 sent", "Last error:   inbox down")

	// A test binary is a temporary build, which install refuses before
	// touching launchd/systemd.
	if schedule.Supported() {
		if _, err := e.run(t, "", "schedule", "install"); err == nil {
			t.Error("install from a test binary succeeded")
		}
	}
	bad := filepath.Join(filepath.Dir(e.cfgPath), "missing.yaml")
	if _, err := runCLI(t, "", "--config", bad, "schedule", "install"); err == nil && runtime.GOOS != "windows" {
		t.Error("install with a missing config succeeded")
	}
	_ = os.WriteFile(filepath.Join(filepath.Dir(e.cfgPath), "auto-state.json"), []byte("{oops"), 0o600)
	if _, err := e.run(t, "", "schedule", "status"); err == nil {
		t.Error("corrupt state file accepted")
	}
}

func TestAutoOnce(t *testing.T) {
	rl := smtptest.Start(t)
	e := newCLIEnv(t, relayConfig(rl, imapInbox(t, [3]string{"privacy@acme.example", "Re: request", "We deleted your data."})))
	t.Setenv("XPC_SERVICE_NAME", "")
	t.Setenv("INVOCATION_ID", "")
	t.Setenv("ERASER_AUTO_MODE", "serve")
	out, err := e.run(t, "", "auto", "--once")
	if err != nil {
		t.Fatalf("auto: %v\n%s", err, out)
	}
	mustContain(t, out, "Eraser cycle", "Complete: 2 sent", "emails from data brokers")
	st, err := schedule.LoadState(filepath.Dir(e.cfgPath))
	if err != nil || st == nil || st.Mode != "serve" || st.Sent != 2 || st.Error != "" {
		t.Errorf("state = %+v, %v", st, err)
	}

	// A held lock makes a second cycle skip quietly.
	release, ok, _ := schedule.TryLock(filepath.Dir(e.cfgPath))
	if !ok {
		t.Fatal("lock not free")
	}
	out, _ = e.run(t, "", "auto", "--once")
	release()
	mustContain(t, out, "already running")

	// Manual mode skips sends; a broken config is recorded as the error.
	m := newCLIEnv(t, manualConfig)
	t.Setenv("ERASER_AUTO_MODE", "")
	out, _ = m.run(t, "", "auto", "--once")
	mustContain(t, out, "send_mode is manual - skipping sends")
	_ = os.WriteFile(m.cfgPath, []byte("profiles: [oops"), 0o600)
	if _, err := m.run(t, "", "auto", "--once"); err == nil {
		t.Error("broken config cycle succeeded")
	}
	if st, _ := schedule.LoadState(filepath.Dir(m.cfgPath)); st == nil || st.Error == "" || st.Mode != "once" {
		t.Errorf("failure not recorded: %+v", st)
	}

	if _, err := m.run(t, "", "auto", "--every", "30m"); err == nil || !strings.Contains(err.Error(), "at least 1h") {
		t.Errorf("short loop interval: %v", err)
	}
}

func TestRotateLog(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auto.log")
	_ = os.WriteFile(path, make([]byte, maxAutoLog+1), 0o600)
	rotateLog(path)
	if _, err := os.Stat(path + ".1"); err != nil {
		t.Error("oversized log not rotated")
	}
	_ = os.WriteFile(path, []byte("small"), 0o600)
	rotateLog(path)
	if _, err := os.Stat(path); err != nil {
		t.Error("small log moved")
	}
}

func TestServeStartupErrors(t *testing.T) {
	e := newCLIEnv(t, "")
	_ = os.WriteFile(e.cfgPath, []byte("profiles: [oops"), 0o600)
	out, err := runCLI(t, "", "--config", e.cfgPath, "--brokers", filepath.Join(t.TempDir(), "missing.yaml"), "serve")
	if err == nil || !strings.Contains(err.Error(), "failed to load brokers") {
		t.Errorf("serve with a missing broker file: %v", err)
	}
	mustContain(t, out, "Config exists but failed to load", "setup wizard")
}

func TestTruncateHelpers(t *testing.T) {
	if got := truncateString("abcdefghij", 6); got != "abc..." {
		t.Errorf("truncateString = %q", got)
	}
	if got := truncateURL("https://broker.example/very/long", 12); got != "https://b..." {
		t.Errorf("truncateURL = %q", got)
	}
	if got := truncateURL("short", 12); got != "short" {
		t.Errorf("truncateURL short = %q", got)
	}
}
