package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/drumandbytes/eraser/internal/config"
)

// Saving inbox settings must set server/port: the form stores straight into
// the live config, and zero values broke the next scan with "dial tcp :0".
// Without JS the server fields post empty, so the preset fills them.
func TestHandleSettingsInboxFillsServerFromPreset(t *testing.T) {
	for _, tc := range []struct {
		provider, server string
		port             int
	}{
		{"gmail", "imap.gmail.com", 993},
		{"proton", "127.0.0.1", 1143},
	} {
		s := newTestServer(t, testConfig())
		s.configPath = filepath.Join(t.TempDir(), "config.yaml")

		form := url.Values{
			"mail_provider": {tc.provider},
			"mail_address":  {"user@example.org"},
			"mail_password": {"app-password"},
		}
		req := httptest.NewRequest(http.MethodPost, "/settings/inbox", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()

		s.handleSettingsInbox(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("%s: expected 200, got %d: %s", tc.provider, w.Code, w.Body.String())
		}
		cfg := s.getConfig()
		if cfg.Inbox.Server != tc.server || cfg.Inbox.Port != tc.port {
			t.Errorf("%s: inbox = %s:%d, want %s:%d", tc.provider, cfg.Inbox.Server, cfg.Inbox.Port, tc.server, tc.port)
		}
		if cfg.Inbox.Email != "user@example.org" || cfg.Inbox.Password != "app-password" || !cfg.Inbox.Enabled {
			t.Errorf("%s: expected submitted account to be saved and enabled, got email=%q password set=%v", tc.provider, cfg.Inbox.Email, cfg.Inbox.Password != "")
		}
	}
}

// A send-only preset (SES) has no IMAP: the form must refuse, not save :0.
func TestHandleSettingsInboxRejectsSendOnlyPreset(t *testing.T) {
	s := newTestServer(t, testConfig())
	s.configPath = filepath.Join(t.TempDir(), "config.yaml")
	form := url.Values{"mail_provider": {"ses"}, "mail_address": {"user@example.org"}, "mail_password": {"pw"}}
	req := httptest.NewRequest(http.MethodPost, "/settings/inbox", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	s.handleSettingsInbox(httptest.NewRecorder(), req)
	if s.getConfig().Inbox.Enabled {
		t.Error("send-only preset enabled an inbox with no server")
	}
}

func postOptions(t *testing.T, s *Server, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/settings/options", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	s.handleSettingsOptions(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	return w
}

func validOptions() url.Values {
	return url.Values{
		"template":            {"ccpa"},
		"rate_limit_ms":       {"1500"},
		"daily_send_limit":    {"100"},
		"broker_list":         {"verified"},
		"regions":             {"eu", "global"},
		"excluded_categories": {" Requires-ID, ,foo "},
	}
}

func TestHandleSettingsOptionsSaves(t *testing.T) {
	s := newTestServer(t, testConfig())
	s.configPath = filepath.Join(t.TempDir(), "config.yaml")
	w := postOptions(t, s, validOptions())
	if !strings.Contains(w.Body.String(), "Request options saved.") {
		t.Fatalf("no success message: %s", w.Body.String())
	}
	o := s.getConfig().Options
	if o.Template != "ccpa" || o.RateLimitMs != 1500 || o.DailySendLimit != 100 || o.BrokerList != "verified" {
		t.Errorf("options not applied: %+v", o)
	}
	if strings.Join(o.Regions, ",") != "eu,global" || strings.Join(o.ExcludedCategories, ",") != "requires-id,foo" {
		t.Errorf("regions/categories wrong: %v %v", o.Regions, o.ExcludedCategories)
	}
}

func TestHandleSettingsOptionsManualSend(t *testing.T) {
	s := newTestServer(t, testConfig())
	s.configPath = filepath.Join(t.TempDir(), "config.yaml")
	form := validOptions()
	form.Set("manual_send", "1")
	postOptions(t, s, form)
	if !s.getConfig().IsManualSend() {
		t.Fatal("manual send not saved")
	}

	// Turning it off with no email account must be refused.
	cfg := *s.getConfig()
	cfg.Email = config.Email{}
	s.config.Store(&cfg)
	w := postOptions(t, s, validOptions())
	if strings.Contains(w.Body.String(), "Request options saved.") || !s.getConfig().IsManualSend() {
		t.Error("left manual mode without an email account")
	}

	// With an account it switches back to SMTP.
	cfg.Email.SMTP.Host = "smtp.example.org"
	s.config.Store(&cfg)
	postOptions(t, s, validOptions())
	if s.getConfig().IsManualSend() {
		t.Error("still manual after unchecking with an account")
	}
}

func TestHandleSettingsOptionsRejectsBadInput(t *testing.T) {
	for name, mutate := range map[string]func(url.Values){
		"template":       func(f url.Values) { f.Set("template", "nope") },
		"negative rate":  func(f url.Values) { f.Set("rate_limit_ms", "-1") },
		"rate not int":   func(f url.Values) { f.Set("rate_limit_ms", "x") },
		"zero daily":     func(f url.Values) { f.Set("daily_send_limit", "0") },
		"broker list":    func(f url.Values) { f.Set("broker_list", "huge") },
		"region":         func(f url.Values) { f.Set("regions", "mars") },
		"custom empty":   func(f url.Values) { f.Set("template", "custom") },
		"custom syntax":  func(f url.Values) { f.Set("custom_template", "Hi {{.FullName") },
		"custom unknown": func(f url.Values) { f.Set("custom_template", "Hi {{.Nope}}") },
	} {
		s := newTestServer(t, testConfig())
		s.configPath = filepath.Join(t.TempDir(), "config.yaml")
		before := s.getConfig().Options
		form := validOptions()
		mutate(form)
		w := postOptions(t, s, form)
		if strings.Contains(w.Body.String(), "Request options saved.") {
			t.Errorf("%s: accepted", name)
		}
		if got := s.getConfig().Options; got.Template != before.Template || got.RateLimitMs != before.RateLimitMs {
			t.Errorf("%s: config changed despite error", name)
		}
	}
}

func TestHandleSettingsOptionsCustomTemplate(t *testing.T) {
	s := newTestServer(t, testConfig())
	s.configPath = filepath.Join(t.TempDir(), "config.yaml")
	form := validOptions()
	form.Set("template", "custom")
	form.Set("custom_template", "Subject: Please erase me\r\n\r\nDear {{.BrokerName}}, from {{.FullName}}")
	postOptions(t, s, form)

	if s.getConfig().Options.Template != "custom" {
		t.Fatal("custom template not selected")
	}
	b := s.brokers().Brokers[0]
	mail, err := s.tmplEngine.Render("custom", s.getConfig().GetProfiles()[0].Profile, b)
	if err != nil {
		t.Fatal(err)
	}
	if mail.Subject != "Please erase me" || !strings.Contains(mail.Body, "Dear "+b.Name+",") {
		t.Errorf("unexpected render: %q / %q", mail.Subject, mail.Body)
	}
}
