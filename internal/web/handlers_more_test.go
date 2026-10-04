package web

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/drumandbytes/eraser/internal/broker"
	"github.com/drumandbytes/eraser/internal/config"
	"github.com/drumandbytes/eraser/internal/history"
	"github.com/drumandbytes/eraser/internal/schedule"
	"github.com/drumandbytes/eraser/internal/smtptest"
)

// sessionRequest builds a request carrying a wizard session set up by fill.
func sessionRequest(t *testing.T, s *Server, method, target string, fill func(*Session)) *http.Request {
	t.Helper()
	req := loopbackRequest(method, target, nil)
	if fill == nil {
		return req
	}
	id, err := s.sessions.Create()
	if err != nil {
		t.Fatal(err)
	}
	s.sessions.Update(id, fill)
	req.AddCookie(&http.Cookie{Name: "eraser_session", Value: id})
	return req
}

func serve(s *Server, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	s.setupRouter().ServeHTTP(rec, req)
	return rec
}

func TestSetupTestPage(t *testing.T) {
	s := newTestServer(t, nil)
	profile := config.Profile{FirstName: "Test", LastName: "User", Email: "test@example.com"}

	rec := serve(s, sessionRequest(t, s, http.MethodGet, "/setup/test", nil))
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/setup/profile" {
		t.Errorf("no session: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	rec = serve(s, sessionRequest(t, s, http.MethodGet, "/setup/test", func(se *Session) { se.Profile = profile }))
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/setup/email" {
		t.Errorf("no email: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	rec = serve(s, sessionRequest(t, s, http.MethodGet, "/setup/test", func(se *Session) {
		se.Profile = profile
		se.Email = config.Email{From: "test@example.com", SMTP: config.SMTPConfig{Host: "smtp.example.com", Port: 465}}
	}))
	if rec.Code != http.StatusOK {
		t.Fatalf("configured: %d", rec.Code)
	}
	if sig, bad := bodyLooksLikeTemplateError(rec.Body.String()); bad {
		t.Fatalf("template error %q", sig)
	}
}

func TestSetupTestSend(t *testing.T) {
	relay := smtptest.Start(t)
	s := newTestServer(t, nil)
	profile := config.Profile{FirstName: "Test", LastName: "User", Email: "me@example.com"}
	withEmail := func(e config.Email) func(*Session) {
		return func(se *Session) { se.Profile, se.Email = profile, e }
	}

	cases := []struct {
		name string
		fill func(*Session)
		code int
		want string
	}{
		{"no session", nil, http.StatusBadRequest, "Email not configured"},
		{"bad provider", withEmail(config.Email{Provider: "fax", SMTP: config.SMTPConfig{Host: "x"}}), http.StatusOK, "Configuration error"},
		{"delivered", withEmail(relay.Email("test@example.com")), http.StatusOK, "Test email sent"},
	}
	for _, c := range cases {
		rec := serve(s, sessionRequest(t, s, http.MethodPost, "/setup/test/send", c.fill))
		if rec.Code != c.code || !strings.Contains(rec.Body.String(), c.want) {
			t.Errorf("%s: %d %s", c.name, rec.Code, rec.Body.String())
		}
	}
	if got := relay.Recipients(); len(got) != 1 || got[0] != "me@example.com" {
		t.Errorf("test email went to %v, want the profile address", got)
	}

	relay.Reject("me@example.com", true)
	rec := serve(s, sessionRequest(t, s, http.MethodPost, "/setup/test/send", withEmail(relay.Email("test@example.com"))))
	if !strings.Contains(rec.Body.String(), "Test failed") {
		t.Errorf("rejected: %s", rec.Body.String())
	}
}

// validConfig passes config.Validate, so schedule.NewJob accepts it.
func validConfig() *config.Config {
	cfg := testConfig()
	cfg.Profile.Country = "Latvia"
	cfg.Email = config.EmailConfig{From: "test@example.com", SMTP: config.SMTPConfig{Host: "smtp.example.com", Port: 465, Username: "u", Password: "p"}}
	cfg.Options.Template = "gdpr"
	return cfg
}

func TestSettingsAutomationActions(t *testing.T) {
	orig := cycleExecutable
	t.Cleanup(func() { cycleExecutable = orig })
	cycleExecutable = func() (string, error) { return "true", nil }

	unconfigured := func() *Server {
		s := newTestServer(t, nil)
		s.configPath = filepath.Join(t.TempDir(), "config.yaml")
		return s
	}
	for _, action := range []string{"enable", "install", "run"} {
		if body := postAutomation(t, unconfigured(), action).Body.String(); !strings.Contains(body, "Finish setup") {
			t.Errorf("%s without config: %s", action, body)
		}
	}
	if body := postAutomation(t, unconfigured(), "bogus").Body.String(); !strings.Contains(body, "Unknown action") {
		t.Errorf("unknown action: %s", body)
	}

	s := newTestServer(t, validConfig())
	s.configPath = filepath.Join(t.TempDir(), "missing-dir", "x", "config.yaml")
	if err := os.WriteFile(filepath.Dir(filepath.Dir(s.configPath)), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if body := postAutomation(t, s, "enable").Body.String(); !strings.Contains(body, "Failed to save configuration") {
		t.Errorf("unsaveable config: %s", body)
	}

	s = newTestServer(t, validConfig())
	s.removeOS = func() error { return nil }
	if body := postAutomation(t, s, "remove").Body.String(); !strings.Contains(body, "Removed the scheduled job") {
		t.Errorf("remove: %s", body)
	}
	s.removeOS = func() error { return errors.New("not loaded") }
	if body := postAutomation(t, s, "remove").Body.String(); !strings.Contains(body, "not loaded") {
		t.Errorf("remove error: %s", body)
	}

	if body := postAutomation(t, s, "run").Body.String(); !strings.Contains(body, "Started a run") {
		t.Errorf("run: %s", body)
	}
	s.jobManager.Create(1, "default")
	if body := postAutomation(t, s, "run").Body.String(); !strings.Contains(body, "already sending") {
		t.Errorf("run during a job: %s", body)
	}

	req := httptest.NewRequest(http.MethodPost, "/settings/automation", strings.NewReader("%zz"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	s.handleSettingsAutomation(rec, req)
	if !strings.Contains(rec.Body.String(), "Failed to parse form") {
		t.Errorf("bad form: %s", rec.Body.String())
	}
}

func TestAutomationView(t *testing.T) {
	cfg := validConfig()
	cfg.Schedule.Enabled = true
	s := newTestServer(t, cfg)
	last := time.Now().Add(-time.Hour).Truncate(time.Second)
	if err := schedule.SaveState(s.dataDir, schedule.State{LastRun: last, Mode: "serve"}); err != nil {
		t.Fatal(err)
	}

	v := s.automationView()
	if !v.Enabled || v.Installed || v.LastVia != "this web app" || !v.Next.Equal(last.Add(schedule.Interval)) {
		t.Errorf("in-app view = %+v", v)
	}

	s.osInstalled = func() bool { return true }
	v = s.automationView()
	if !v.Installed || !v.Next.After(time.Now()) {
		t.Errorf("OS view = %+v, want the next OS run", v)
	}
}

func TestSettingsBrokersUpdateOutcomes(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	var list strings.Builder
	list.WriteString("brokers:\n")
	for i := 0; i < broker.MinSaneBrokerCount+1; i++ {
		fmt.Fprintf(&list, "  - {id: b%d, name: B%d, email: b%d@example.com, region: eu}\n", i, i, i)
	}
	status, etag := http.StatusOK, `"v1"`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if status == http.StatusOK && r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", etag)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(list.String()))
	}))
	t.Cleanup(srv.Close)

	update := func(s *Server) string {
		rec := httptest.NewRecorder()
		s.handleSettingsBrokersUpdate(rec, httptest.NewRequest(http.MethodPost, "/settings/brokers/update", nil))
		return rec.Body.String()
	}

	s := smokeServer(t)
	s.brokerUpdateURL = srv.URL
	cfg := *s.getConfig()
	cfg.Options.BrokerList = "verified"
	s.config.Store(&cfg)
	if body := update(s); !strings.Contains(body, "Broker list updated") || !strings.Contains(body, "sends to a different list") {
		t.Errorf("first update: %s", body)
	}
	if body := update(s); !strings.Contains(body, "already up to date") {
		t.Errorf("second update: %s", body)
	}

	status = http.StatusInternalServerError
	if body := update(s); !strings.Contains(body, "Update failed") {
		t.Errorf("server error: %s", body)
	}

	// A fresh list, but the --brokers override it reloads from is gone.
	status, etag = http.StatusOK, `"v2"`
	list.WriteString("  - {id: extra, name: Extra, email: extra@example.com, region: eu}\n")
	s.BrokerOverride = filepath.Join(t.TempDir(), "gone.yaml")
	if body := update(s); !strings.Contains(body, "reloading failed") {
		t.Errorf("reload failure: %s", body)
	}
}

func TestMarkSentAndBouncedEndpoints(t *testing.T) {
	s := smokeServer(t)
	if rec := do(t, s, http.MethodPost, "/api/brokers/spokeo/mark-sent", true); rec.Code != http.StatusOK {
		t.Fatalf("mark-sent: %d %s", rec.Code, rec.Body.String())
	}
	rec := do(t, s, http.MethodPost, "/api/brokers/spokeo/mark-bounced", true)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "badge-error") {
		t.Fatalf("mark-bounced: %d %s", rec.Code, rec.Body.String())
	}
	recs, _ := s.historyStore.GetAllRequests("default")
	if len(recs) != 1 || recs[0].Status != history.StatusFailed || recs[0].SentMethod != "manual" {
		t.Errorf("history = %+v", recs)
	}

	for _, action := range []string{"mark-sent", "mark-bounced"} {
		if rec := do(t, s, http.MethodPost, "/api/brokers/ghost/"+action, true); rec.Code != http.StatusNotFound {
			t.Errorf("%s unknown broker: %d", action, rec.Code)
		}
		if rec := do(t, nilStoreServer(t), http.MethodPost, "/api/brokers/spokeo/"+action, true); rec.Code != http.StatusInternalServerError {
			t.Errorf("%s without a db: %d", action, rec.Code)
		}
		broken := smokeServer(t)
		_ = broken.historyStore.Close()
		if rec := do(t, broken, http.MethodPost, "/api/brokers/spokeo/"+action, true); rec.Code != http.StatusInternalServerError {
			t.Errorf("%s with a closed db: %d", action, rec.Code)
		}
	}
	if rec := do(t, s, http.MethodGet, "/api/brokers/ghost/status", true); rec.Code != http.StatusNotFound {
		t.Errorf("status of unknown broker: %d", rec.Code)
	}
}

func TestExportEndpoint(t *testing.T) {
	s := smokeServer(t)
	if err := s.historyStore.Add(&history.Record{ProfileID: "default", BrokerID: "spokeo", BrokerName: "Spokeo",
		Email: "privacy@spokeo.com", Template: "gdpr", Status: history.StatusSent, SentAt: time.Now()}); err != nil {
		t.Fatal(err)
	}

	rec := do(t, s, http.MethodGet, "/export?format=json&since=2020-01-01", false)
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "application/json" ||
		!strings.Contains(rec.Header().Get("Content-Disposition"), "eraser-evidence-default-") ||
		!strings.Contains(rec.Body.String(), `"broker_id": "spokeo"`) {
		t.Fatalf("json export: %d %q %s", rec.Code, rec.Header().Get("Content-Disposition"), rec.Body.String())
	}
	rec = do(t, s, http.MethodGet, "/export", false)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "evidence report") {
		t.Fatalf("html export: %d", rec.Code)
	}

	for target, code := range map[string]int{
		"/export?format=xml":       http.StatusBadRequest,
		"/export?since=yesterday":  http.StatusBadRequest,
		"/export?format=json&x=1":  http.StatusOK,
		"/export?format=html&x=1":  http.StatusOK,
		"/export?since=2020-13-45": http.StatusBadRequest,
	} {
		if rec := do(t, s, http.MethodGet, target, false); rec.Code != code {
			t.Errorf("%s: %d, want %d", target, rec.Code, code)
		}
	}
	if rec := do(t, nilStoreServer(t), http.MethodGet, "/export", false); rec.Code != http.StatusInternalServerError {
		t.Errorf("without a db: %d", rec.Code)
	}
	broken := smokeServer(t)
	_ = broken.historyStore.Close()
	if rec := do(t, broken, http.MethodGet, "/export", false); rec.Code != http.StatusInternalServerError {
		t.Errorf("closed db: %d", rec.Code)
	}
}

func TestMailOverrideViewPrefills(t *testing.T) {
	e := config.EmailConfig{From: "me@fastmail.example", SMTP: config.SMTPConfig{Host: "smtp.fastmail.com", Port: 465, Username: "me", Password: "secret"}}
	v := mailOverrideView(&config.MailConfig{Email: &e})
	if v.PasswordPlaceholder != "Leave blank to keep current" {
		t.Errorf("placeholder = %q", v.PasswordPlaceholder)
	}
	if blank := mailOverrideView(nil); blank.PasswordPlaceholder == v.PasswordPlaceholder {
		t.Error("no override should not claim a saved password")
	}
}

func TestRenderUnknownTemplates(t *testing.T) {
	s := newTestServer(t, testConfig())
	rec := httptest.NewRecorder()
	s.renderPartial(rec, "partials/nope.html", nil)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("renderPartial: %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	s.renderWithCSRF(rec, loopbackRequest(http.MethodGet, "/", nil), "nope.html", map[string]interface{}{})
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("renderWithCSRF: %d", rec.Code)
	}
	if got := s.getRecentHistory("default", 5); got != nil {
		t.Errorf("getRecentHistory without a db = %v", got)
	}
}

func TestExportPreservesProfileScope(t *testing.T) {
	s := smokeServer(t)
	if err := s.historyStore.Add(&history.Record{ProfileID: "spouse", BrokerID: "spokeo", BrokerName: "Spokeo",
		Email: "privacy@spokeo.com", Template: "gdpr", Status: history.StatusSent, SentAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	req := loopbackRequest(http.MethodGet, "/export?format=json", nil)
	req.AddCookie(&http.Cookie{Name: activeProfileCookie, Value: "default"})
	rec := serve(s, req)
	if strings.Contains(rec.Body.String(), `"broker_id": "spokeo"`) {
		t.Error("export leaked another profile's requests")
	}
}
