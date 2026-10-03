package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/drumandbytes/eraser/internal/broker"
	"github.com/drumandbytes/eraser/internal/config"
	"github.com/drumandbytes/eraser/internal/schedule"
)

func TestCycleDue(t *testing.T) {
	s := newTestServer(t, testConfig())
	if s.cycleDue() {
		t.Fatal("due with schedule.enabled off")
	}

	cfg := *s.getConfig()
	cfg.Schedule.Enabled = true
	s.config.Store(&cfg)
	if !s.cycleDue() {
		t.Fatal("not due when enabled and no cycle has ever run")
	}

	if err := schedule.SaveState(s.dataDir, schedule.State{LastRun: time.Now().Add(-time.Hour), Mode: "os"}); err != nil {
		t.Fatal(err)
	}
	if s.cycleDue() {
		t.Fatal("due an hour after the last cycle")
	}
	if err := schedule.SaveState(s.dataDir, schedule.State{LastRun: time.Now().Add(-schedule.Interval - time.Minute)}); err != nil {
		t.Fatal(err)
	}
	if !s.cycleDue() {
		t.Fatal("not due after a full interval")
	}

	s.osInstalled = func() bool { return true }
	if s.cycleDue() {
		t.Fatal("serve scheduled a cycle while the OS job is installed")
	}
}

func postAutomation(t *testing.T, s *Server, action string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/settings/automation", strings.NewReader(url.Values{"action": {action}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	s.handleSettingsAutomation(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("%s: status %d: %s", action, w.Code, w.Body.String())
	}
	return w
}

func TestSettingsAutomationToggleSavesConfig(t *testing.T) {
	s := newTestServer(t, testConfig())
	s.configPath = filepath.Join(t.TempDir(), "config.yaml")

	postAutomation(t, s, "enable")
	saved, err := config.Load(s.configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !saved.Schedule.Enabled || !s.getConfig().Schedule.Enabled {
		t.Fatal("enable didn't set schedule.enabled on disk and in the live config")
	}

	postAutomation(t, s, "disable")
	if s.getConfig().Schedule.Enabled {
		t.Fatal("disable left schedule.enabled on")
	}
}

func TestSettingsAutomationInstallRefusesInvalidConfig(t *testing.T) {
	if !schedule.Supported() {
		t.Skip("no OS scheduler on this platform")
	}
	s := newTestServer(t, testConfig()) // no email: block, so Validate fails
	s.configPath = filepath.Join(t.TempDir(), "config.yaml")
	installed := false
	s.installOS = func(schedule.Job) error { installed = true; return nil }

	w := postAutomation(t, s, "install")
	if installed {
		t.Fatal("installed an OS job for a config that fails validation")
	}
	if !strings.Contains(w.Body.String(), "Couldn&#39;t install") {
		t.Fatalf("no error shown: %s", w.Body.String())
	}
}

func TestSendAllRefusedWhileCycleRuns(t *testing.T) {
	cfg := testConfig()
	cfg.Email = config.EmailConfig{From: "test@example.com", SMTP: config.SMTPConfig{Host: "smtp.example.com", Port: 465, Username: "u", Password: "p"}}
	s := newTestServer(t, cfg)
	s.brokerDB = &broker.BrokerDatabase{Brokers: []broker.Broker{{ID: "acme", Name: "Acme", Email: "privacy@acme.example", Region: "eu"}}}
	release, ok, err := schedule.TryLock(s.dataDir)
	if err != nil || !ok {
		t.Fatalf("TryLock: %v %v", ok, err)
	}
	defer release()

	w := httptest.NewRecorder()
	s.handleAPISendAll(w, httptest.NewRequest(http.MethodPost, "/api/send-all", nil))
	if w.Code != http.StatusConflict {
		t.Fatalf("status %d, want 409: %s", w.Code, w.Body.String())
	}
}
