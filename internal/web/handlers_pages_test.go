package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/drumandbytes/eraser/internal/config"
)

// TestPipelineShowsScanButtonForProfileMailOverride is a regression test:
// handlePipeline used to gate the "Scan Inbox" button on the shared
// cfg.Inbox.Enabled, so a profile relying only on its own mail.inbox
// override (with no shared inbox: block configured at all) would never see
// the button, even though the API scan handler it points at already
// resolves the inbox per profile (see handleAPIInboxScan).
func TestPipelineShowsScanButtonForProfileMailOverride(t *testing.T) {
	s := newTestServer(t, &config.Config{
		Profiles: []config.NamedProfile{{
			ID:      "spouse",
			Profile: config.Profile{FirstName: "Spouse", LastName: "User", Email: "spouse@example.com"},
			Mail: &config.MailConfig{
				Inbox: &config.InboxConfig{Enabled: true, Provider: "gmail", Email: "spouse@gmail.com", Password: "app-password"},
			},
		}},
		// No shared inbox: block at all - the button must come from the
		// profile's own override, not a shared default.
	})

	req := httptest.NewRequest(http.MethodGet, "/pipeline", nil)
	req.AddCookie(&http.Cookie{Name: activeProfileCookie, Value: "spouse"})
	w := httptest.NewRecorder()
	s.handlePipeline(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `id="scan-inbox-btn"`) {
		t.Error("expected the Scan Inbox button to render for a profile with its own configured mail.inbox override")
	}
}
