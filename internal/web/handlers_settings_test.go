package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
)

// Saving inbox settings must set Gmail server/port: the form stores straight
// into the live config, and zero values broke the next scan with "dial tcp :0".
func TestHandleSettingsInboxSetsGmailServerDefaults(t *testing.T) {
	s := newTestServer(t, testConfig())
	s.configPath = filepath.Join(t.TempDir(), "config.yaml")

	form := url.Values{
		"inbox_email":    {"user@gmail.com"},
		"inbox_password": {"app-password"},
	}
	req := httptest.NewRequest(http.MethodPost, "/settings/inbox", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()

	s.handleSettingsInbox(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	cfg := s.getConfig()
	if cfg == nil {
		t.Fatal("expected a non-nil config after saving inbox settings")
	}
	if cfg.Inbox.Server == "" || cfg.Inbox.Port == 0 {
		t.Errorf("expected a non-empty IMAP server/port, got server=%q port=%d - this is the exact state that produced \"dial tcp :0\"", cfg.Inbox.Server, cfg.Inbox.Port)
	}
	if cfg.Inbox.Server != "imap.gmail.com" || cfg.Inbox.Port != 993 {
		t.Errorf("expected Gmail IMAP defaults imap.gmail.com:993, got %s:%d", cfg.Inbox.Server, cfg.Inbox.Port)
	}
	if cfg.Inbox.Email != "user@gmail.com" || cfg.Inbox.Password != "app-password" {
		t.Errorf("expected submitted email/password to be preserved, got email=%q password set=%v", cfg.Inbox.Email, cfg.Inbox.Password != "")
	}
}
