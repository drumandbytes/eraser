package main

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/drumandbytes/eraser/internal/broker"
	"github.com/drumandbytes/eraser/internal/browser"
	"github.com/drumandbytes/eraser/internal/config"
	"github.com/drumandbytes/eraser/internal/history"
	"github.com/drumandbytes/eraser/internal/inbox"
	"github.com/drumandbytes/eraser/internal/schedule"
	"github.com/drumandbytes/eraser/internal/smtptest"
	"github.com/drumandbytes/eraser/internal/web"
)

// blockHistory puts a directory where history.db goes, so opening it fails.
func blockHistory(t *testing.T, e *cliEnv) {
	t.Helper()
	if err := os.Mkdir(history.DBPathFor(e.cfgPath), 0o700); err != nil {
		t.Fatal(err)
	}
}

// The commands that read config and history report each failure cleanly.
func TestCommandSetupErrors(t *testing.T) {
	missing := newCLIEnv(t, "")
	missingBrokers := filepath.Join(t.TempDir(), "nope.yaml")
	for _, args := range [][]string{
		{"send"}, {"export", "-o", "x"}, {"monitor"}, {"fill", "--url", "x"}, {"confirm", "--url", "x"},
		{"mark-sent", "acme"}, {"draft", "acme"}, {"mark-bounced", "acme"}, {"cleanup-bounces"},
	} {
		if _, err := missing.run(t, "", args...); err == nil || !strings.Contains(err.Error(), "config") {
			t.Errorf("%v without config: %v", args, err)
		}
	}

	rl := smtptest.Start(t)
	multi := newCLIEnv(t, manualConfig)
	for _, args := range [][]string{{"send"}, {"export", "-o", "x"}, {"fill", "--url", "x"}, {"confirm", "--url", "x"}, {"draft", "acme"}, {"mark-sent", "acme"}} {
		if _, err := multi.run(t, "", args...); err == nil || !strings.Contains(err.Error(), "multiple profiles") {
			t.Errorf("%v with two profiles and no --profile: %v", args, err)
		}
	}

	for _, args := range [][]string{{"send"}, {"export", "-o", "x"}, {"monitor"}, {"fill", "--url", "x"}, {"confirm", "--url", "x"}, {"draft", "acme"}, {"mark-sent", "acme"}} {
		e := newCLIEnv(t, relayConfig(rl, imapInbox(t)))
		if _, err := runCLI(t, "", append([]string{"--config", e.cfgPath, "--brokers", missingBrokers}, args...)...); err == nil || !strings.Contains(err.Error(), "brokers") {
			t.Errorf("%v with a missing broker list: %v", args, err)
		}
	}

	for _, args := range [][]string{{"send"}, {"export", "-o", "x"}, {"monitor"}, {"fill", "--url", "x"}, {"confirm", "--url", "x"}, {"mark-sent", "acme"}, {"mark-bounced", "acme"}, {"cleanup-bounces"}, {"status"}, {"pipeline"}} {
		e := newCLIEnv(t, relayConfig(rl, imapInbox(t)))
		blockHistory(t, e)
		if _, err := e.run(t, "", args...); err == nil || !strings.Contains(err.Error(), "history") {
			t.Errorf("%v with an unopenable history: %v", args, err)
		}
	}
}

func TestSendOutputBranches(t *testing.T) {
	rl := smtptest.Start(t)
	cfg := strings.Replace(relayConfig(rl, ""), "profiles:\n", "profiles:\n  - id: spouse\n    first_name: John\n    last_name: Doe\n    email: john@example.com\n", 1)
	e := newCLIEnv(t, cfg)
	if err := os.WriteFile(e.brokersPath, []byte(testBrokersYAML+"  - {id: formonly, name: Form Only, opt_out_url: https://form.example/optout, region: eu}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := e.run(t, "", "--profile", "spouse", "send", "--status", "")
	if err != nil {
		t.Fatal(err)
	}
	mustContain(t, out, "Profile: spouse (John Doe)", "use the opt-out form instead: https://form.example/optout")

	out, _ = e.run(t, lines("q"), "--profile", "spouse", "send", "--manual", "--broker", "formonly")
	mustContain(t, out, "No email on file - use the opt-out form instead:")

	bad := newCLIEnv(t, strings.Replace(relayConfig(rl, ""), "template: gdpr", "template: nope", 1))
	out, _ = bad.run(t, "", "send", "--dry-run")
	mustContain(t, out, "Failed to render template")
	out, _ = bad.run(t, "", "send")
	mustContain(t, out, "unknown template", "Complete: 0 sent, 2 failed")
	out, _ = bad.run(t, lines("n", "n", "n"), "send", "--manual")
	mustContain(t, out, "Failed to render")
}

func TestExportErrorsAndDefaultPath(t *testing.T) {
	e := newCLIEnv(t, manualConfig)
	t.Chdir(t.TempDir())
	out, err := e.run(t, "", "--profile", "default", "export")
	if err != nil {
		t.Fatal(err)
	}
	name := "eraser-evidence-default-" + time.Now().Format("2006-01-02") + ".html"
	if _, err := os.Stat(name); err != nil {
		t.Errorf("default output %s not written: %v\n%s", name, err, out)
	}
	if _, err := e.run(t, "", "--profile", "default", "export", "--format", "xml"); err == nil {
		t.Error("unknown format accepted")
	}
	blocker := filepath.Join(t.TempDir(), "file")
	_ = os.WriteFile(blocker, nil, 0o600)
	if _, err := e.run(t, "", "--profile", "default", "export", "-o", filepath.Join(blocker, "x.html")); err == nil {
		t.Error("export under a file succeeded")
	}
}

func TestUpdateBrokersCheckAndOwnEntries(t *testing.T) {
	newCLIEnv(t, "")
	var list strings.Builder
	list.WriteString("brokers:\n")
	for i := 0; i < broker.MinSaneBrokerCount; i++ {
		fmt.Fprintf(&list, "  - {id: b%d, name: B%d, email: b%d@example.com, region: eu}\n", i, i, i)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(list.String())) }))
	defer srv.Close()

	out, err := runCLI(t, "", "update-brokers", "--url", srv.URL, "--check")
	if !errors.Is(err, errUpdateAvailable) || !strings.Contains(out, "A newer broker list is available") {
		t.Errorf("--check: %v %s", err, out)
	}
	if err := broker.SaveLocal(broker.Broker{ID: "mine", Name: "Mine", Email: "a@mine.example", Region: "eu"}); err != nil {
		t.Fatal(err)
	}
	out, err = runCLI(t, "", "update-brokers", "--url", srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	mustContain(t, out, "+ 1 of your own, kept")
	if _, err := runCLI(t, "", "update-brokers", "--url", "http://127.0.0.1:1/x"); err == nil {
		t.Error("unreachable URL accepted")
	}
}

// stubBrowser returns canned results so fill's result handling runs without Chrome.
type stubBrowser struct {
	results map[string]*browser.FormResult
	err     error
}

func (s *stubBrowser) NavigateAndFill(url, brokerID string, _ bool) (*browser.FormResult, error) {
	if s.err != nil {
		return &browser.FormResult{URL: url}, s.err
	}
	return s.results[url], nil
}
func (s *stubBrowser) Close() {}

func TestFillResults(t *testing.T) {
	stub := &stubBrowser{results: map[string]*browser.FormResult{
		"https://acme.example/captcha": {CaptchaFound: true, CaptchaType: "hcaptcha", FieldsFilled: []string{"email"}, FieldsMissing: []string{"phone"}, ScreenshotPath: "acme.png"},
		"https://acme.example/submit":  {SubmitAttempted: true, Success: true, FieldsFilled: []string{"email", "name"}},
		"https://acme.example/filled":  {Success: true, FieldsFilled: []string{"email"}},
		"https://acme.example/partial": {ErrorMessage: "submit failed: no button"},
	}}
	var gotCfg browser.BrowserConfig
	var gotDomains []string
	orig := newFormBrowser
	t.Cleanup(func() { newFormBrowser = orig })
	newFormBrowser = func(cfg browser.BrowserConfig, _ *config.Profile, domains []string) (formBrowser, error) {
		gotCfg, gotDomains = cfg, domains
		return stub, nil
	}

	e := newCLIEnv(t, manualConfig+"pipeline:\n  browser_headless: false\n  browser_timeout_sec: 12\n")
	if err := os.WriteFile(e.brokersPath, []byte(testBrokersYAML+"  - {id: www, name: WWW Co, email: a@www.example, website: https://www.wwwco.example/privacy, region: eu}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, _ := history.NewStore(history.DBPathFor(e.cfgPath))
	for _, path := range []string{"captcha", "submit", "filled", "partial"} {
		_ = store.AddBrokerResponse(&history.BrokerResponse{ProfileID: "default", BrokerID: "acme", BrokerName: "Acme Data",
			ResponseType: "form_required", FormURL: "https://acme.example/" + path, EmailSubject: path})
	}
	_ = store.Close()

	out, err := e.run(t, "", "--profile", "default", "fill", "--pending", "--submit")
	if err != nil {
		t.Fatal(err)
	}
	mustContain(t, out, "Forms to process: 4", "CAPTCHA detected: hcaptcha", "Created CAPTCHA task", "Missing profile data for: phone",
		"Screenshot: acme.png", "Form submitted!", "Form filled (not submitted)", "Processed 4 forms")
	if gotCfg.Headless || gotCfg.Timeout != 12*time.Second || !strings.HasSuffix(gotCfg.ScreenshotDir, filepath.Join(".eraser", "screenshots")) {
		t.Errorf("browser config = %+v", gotCfg)
	}
	if !strings.Contains(strings.Join(gotDomains, ","), "wwwco.example") {
		t.Errorf("www. website not allowlisted bare: %v", gotDomains)
	}
	store, _ = history.NewStore(history.DBPathFor(e.cfgPath))
	tasks, _ := store.GetPendingTasks("default", history.TaskCaptcha, "pending")
	_ = store.Close()
	if len(tasks) != 1 || tasks[0].ScreenshotPath != "acme.png" || !strings.Contains(tasks[0].BrowserState, "jane@example.com") {
		t.Errorf("captcha task = %+v", tasks)
	}

	// --wait wires a prompt that waits for Enter.
	newFormBrowser = func(cfg browser.BrowserConfig, _ *config.Profile, _ []string) (formBrowser, error) {
		if !cfg.WaitForUser || cfg.WaitCallback() != nil {
			t.Error("--wait callback not set up or didn't read Enter")
		}
		return stub, nil
	}
	out, _ = e.run(t, lines(""), "--profile", "default", "fill", "--url", "https://acme.example/filled", "--wait", "--headless=false")
	mustContain(t, out, "Press ENTER when done")

	newFormBrowser = func(browser.BrowserConfig, *config.Profile, []string) (formBrowser, error) {
		return nil, errors.New("no chrome")
	}
	if _, err := e.run(t, "", "--profile", "default", "fill", "--url", "https://acme.example/filled"); err == nil || !strings.Contains(err.Error(), "failed to create browser") {
		t.Errorf("browser failure: %v", err)
	}
}

func TestAutoLoopAndModes(t *testing.T) {
	origWait := waitForNextCycle
	t.Cleanup(func() { waitForNextCycle = origWait })
	waits := 0
	waitForNextCycle = func(time.Duration) bool { waits++; return waits >= 2 }

	rl := smtptest.Start(t)
	e := newCLIEnv(t, relayConfig(rl, ""))
	t.Setenv("ERASER_AUTO_MODE", "")
	t.Setenv("INVOCATION_ID", "")
	t.Setenv("XPC_SERVICE_NAME", "")
	out, err := e.run(t, "", "auto", "--every", "2h")
	if err != nil {
		t.Fatal(err)
	}
	if waits != 2 || strings.Count(out, "Eraser cycle") != 2 || !strings.Contains(out, "Next cycle at") {
		t.Errorf("loop ran %d waits:\n%s", waits, out)
	}
	if st, _ := schedule.LoadState(filepath.Dir(e.cfgPath)); st == nil || st.Mode != "loop" {
		t.Errorf("state = %+v", st)
	}

	// A failing cycle is reported and the loop keeps going.
	waits = 0
	broken := newCLIEnv(t, "profiles: [oops")
	if _, err := broken.run(t, "", "auto"); err != nil {
		t.Errorf("loop with a failing cycle: %v", err)
	}

	t.Setenv("INVOCATION_ID", "systemd")
	_, _ = e.run(t, "", "auto", "--once")
	if st, _ := schedule.LoadState(filepath.Dir(e.cfgPath)); st.Mode != "os" {
		t.Errorf("mode under the OS job = %q", st.Mode)
	}
	t.Setenv("INVOCATION_ID", "")

	// The OS job already installed: looping here would double up.
	home := os.Getenv("HOME") // the last newCLIEnv's
	unit := filepath.Join(home, "Library", "LaunchAgents", "com.drumandbytes.eraser.auto.plist")
	if runtime.GOOS == "linux" {
		unit = filepath.Join(home, ".config", "systemd", "user", "eraser-auto.timer")
	}
	if schedule.Supported() {
		_ = os.MkdirAll(filepath.Dir(unit), 0o700)
		_ = os.WriteFile(unit, nil, 0o600)
		if _, err := e.run(t, "", "auto"); err == nil || !strings.Contains(err.Error(), "already runs these cycles") {
			t.Errorf("loop with the OS job installed: %v", err)
		}
		out, _ := e.run(t, "", "schedule", "status")
		mustContain(t, out, "OS scheduler: installed")
	}
}

func TestAutoCycleFailures(t *testing.T) {
	rl := smtptest.Start(t)

	blocker := filepath.Join(t.TempDir(), "file")
	_ = os.WriteFile(blocker, nil, 0o600)
	if _, err := runCLI(t, "", "--config", filepath.Join(blocker, "config.yaml"), "auto", "--once"); err == nil {
		t.Error("config dir under a file accepted")
	}

	e := newCLIEnv(t, relayConfig(rl, ""))
	dir := filepath.Dir(e.cfgPath)
	_ = os.Mkdir(filepath.Join(dir, "auto.lock"), 0o700)
	if _, err := e.run(t, "", "auto", "--once"); err == nil || !strings.Contains(err.Error(), "lock") {
		t.Errorf("unopenable lock: %v", err)
	}

	e = newCLIEnv(t, relayConfig(rl, "inbox:\n  enabled: true\n  server: 127.0.0.1\n  port: 1\n  email: a@b.example\n  password: p\n"))
	_ = os.Mkdir(filepath.Join(filepath.Dir(e.cfgPath), "auto-state.json.tmp"), 0o700)
	if _, err := e.run(t, "", "auto", "--once"); err == nil || !strings.Contains(err.Error(), "inbox") {
		t.Errorf("unreachable inbox in a cycle: %v", err)
	}

	noSend := newCLIEnv(t, "profiles:\n  - id: default\n    first_name: A\n    last_name: B\n    email: a@b.example\n")
	if _, err := noSend.run(t, "", "auto", "--once"); err == nil || !strings.Contains(err.Error(), "send (default)") {
		t.Errorf("invalid send config in a cycle: %v", err)
	}
	if sentSince(filepath.Join(t.TempDir(), "missing.yaml"), time.Now()) != 0 {
		t.Error("sentSince without a config")
	}
	fresh := newCLIEnv(t, manualConfig)
	blockHistory(t, fresh)
	if sentSince(fresh.cfgPath, time.Now()) != 0 {
		t.Error("sentSince without a history")
	}
}

func TestMonitorExtras(t *testing.T) {
	// Two profiles with their own inboxes, and auto-archiving.
	a := imapInbox(t, [3]string{"privacy@acme.example", "Re: request", "Your request is pending review; we will reply shortly."})
	b := imapInbox(t, [3]string{"dpo@globex.example", "Re: request", "We cannot process your request."})
	port := func(inboxYAML string) string {
		i := strings.Index(inboxYAML, "port: ") + len("port: ")
		return strings.TrimSpace(inboxYAML[i : i+strings.Index(inboxYAML[i:], "\n")])
	}
	profile := func(id, email, p string) string {
		return "  - id: " + id + "\n    first_name: X\n    last_name: Doe\n    email: " + email + "\n" +
			"    mail:\n      email: {from: " + email + ", smtp: {host: smtp.example.com, port: 465}}\n" +
			"      inbox: {enabled: true, server: 127.0.0.1, port: " + p + ", email: " + email + ", password: password, auto_archive: true}\n"
	}
	cfg := "profiles:\n" + profile("default", "username", port(a)) + profile("spouse", "username2", port(b)) + "options:\n  send_mode: manual\n"
	e := newCLIEnv(t, cfg)
	out, err := e.run(t, "", "monitor")
	if err != nil {
		t.Fatalf("monitor: %v\n%s\n%s", err, out, cfg)
	}
	mustContain(t, out, "Monitoring 2 configured inboxes", "Archived 1 emails")

	for typ, icon := range map[inbox.ResponseType]string{inbox.ResponsePending: "⏳", inbox.ResponseRejected: "❌", inbox.ResponseUnknown: "❓", inbox.ResponseConfirmationRequired: "🔗"} {
		got := captureStdout(t, func() {
			printClassifiedResponse(inbox.ClassifiedResponse{Type: typ, Email: &inbox.Email{BrokerName: "X"}, ConfirmURL: "https://x.example/c", NeedsReview: true})
		})
		if !strings.Contains(got, icon) {
			t.Errorf("%s: %q", typ, got)
		}
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stdout
	os.Stdout = w
	fn()
	os.Stdout = orig
	_ = w.Close()
	out, _ := io.ReadAll(r)
	return string(out)
}

// serve runs until Ctrl+C, then shuts down cleanly.
func TestServeUntilInterrupted(t *testing.T) {
	orig := web.OpenBrowser
	t.Cleanup(func() { web.OpenBrowser = orig })
	web.OpenBrowser = func(string) {}

	e := newCLIEnv(t, manualConfig)
	port := strconv.Itoa(freeTCPPort(t))
	done := make(chan error, 1)
	var out string
	go func() {
		var err error
		out, err = e.run(t, "", "serve", "--port", port)
		done <- err
	}()
	deadline := time.Now().Add(10 * time.Second)
	for {
		resp, err := http.Get("http://127.0.0.1:" + port + "/")
		if err == nil {
			_ = resp.Body.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("serve never came up: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err := syscall.Kill(os.Getpid(), syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("serve didn't stop on SIGINT")
	}
	mustContain(t, out, "Starting Eraser web UI", "Shutting down...")
}

func freeTCPPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	return ln.Addr().(*net.TCPAddr).Port
}
