package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
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
