package web

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/drumandbytes/eraser/internal/config"
)

func dashboard(t *testing.T, s *Server) string {
	t.Helper()
	rec := httptest.NewRecorder()
	s.setupRouter().ServeHTTP(rec, loopbackRequest(http.MethodGet, "/", nil))
	return rec.Body.String()
}

func TestWhatsNewShowsOnceOnTheDashboard(t *testing.T) {
	s := smokeServer(t)
	body := dashboard(t, s)
	if !strings.Contains(body, "What's new in Eraser 2.0") || !strings.Contains(body, "upgrading-to-2.0.md") {
		t.Fatalf("card missing from the dashboard:\n%.400s", body)
	}
	if strings.Contains(dashboardOther(t, s), "What's new in Eraser 2.0") {
		t.Error("the card belongs on the dashboard only")
	}

	rec := post(t, s, "/whats-new/dismiss", nil)
	if rec.Code != http.StatusSeeOther || !config.WhatsNewSeen(s.configPath) {
		t.Fatalf("dismiss = %d seen=%v", rec.Code, config.WhatsNewSeen(s.configPath))
	}
	if strings.Contains(dashboard(t, s), "What's new in Eraser 2.0") {
		t.Error("card shown again after dismissal")
	}
}

func dashboardOther(t *testing.T, s *Server) string {
	t.Helper()
	rec := httptest.NewRecorder()
	s.setupRouter().ServeHTTP(rec, loopbackRequest(http.MethodGet, "/history", nil))
	return rec.Body.String()
}

func TestWhatsNewAbsorbsTheUpgradeBanner(t *testing.T) {
	s := smokeServer(t)
	if err := os.WriteFile(s.configPath+".1x.bak", []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	body := dashboard(t, s)
	if strings.Count(body, "Config upgraded") != 0 || !strings.Contains(body, "Your config was upgraded.") {
		t.Errorf("the dashboard should show one notice that includes the migration note")
	}
	// Other pages keep the plain banner until it's dismissed.
	if !strings.Contains(dashboardOther(t, s), "Config upgraded") {
		t.Error("the banner should still show off the dashboard")
	}
	post(t, s, "/whats-new/dismiss", nil)
	if s.upgradeNotice() != nil {
		t.Error("dismissing the card should settle the migration note too")
	}
}

func TestWhatsNewNotForFreshInstalls(t *testing.T) {
	if (&Server{}).whatsNew() != nil {
		t.Error("no config path: nothing to show")
	}
	s := smokeServer(t)
	config.MarkWhatsNewSeen(s.configPath)
	if s.whatsNew() != nil {
		t.Error("already seen")
	}
	empty := newTestServer(t, &config.Config{})
	empty.configPath = s.configPath + ".other"
	if empty.whatsNew() != nil {
		t.Error("no profile yet (setup not done): nothing to show")
	}
}
