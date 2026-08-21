package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
)

// TestHandleSettingsInboxSetsGmailServerDefaults guards against a
// regression where saving inbox settings from the web UI wrote an
// InboxConfig with Server/Port left at their zero values (settings.html
// only collects email/password - it's a Gmail-only form and never asked
// for a server/port). That produced a live in-memory config (and a saved
// config.yaml) with server="" port=0, which broke the next inbox
// scan/monitor with "dial tcp :0: connect: can't assign requested
// address" until the process was restarted (config.Load's own
// default-filling only runs at startup, not when handleSettingsInbox
// stores directly into the already-running server's config).
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
