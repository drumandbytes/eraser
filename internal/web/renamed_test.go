package web

import (
	"net/http"
	"strings"
	"testing"

	"github.com/drumandbytes/delistd/internal/config"
)

func TestRenameNoticeShowsOnceOnTheDashboard(t *testing.T) {
	s := smokeServer(t)
	// the 2.0 card takes the spot first for an upgrader who hasn't seen it
	if s.renameNotice() || strings.Contains(dashboard(t, s), "Eraser is now Delistd") {
		t.Fatal("rename card must wait for the 2.0 card")
	}
	config.MarkWhatsNewSeen(s.configPath)
	if !strings.Contains(dashboard(t, s), "Eraser is now Delistd") {
		t.Fatal("rename card missing from the dashboard")
	}
	rec := post(t, s, "/renamed/dismiss", nil)
	if rec.Code != http.StatusSeeOther || !config.RenameNoticeSeen(s.configPath) {
		t.Fatalf("dismiss = %d seen=%v", rec.Code, config.RenameNoticeSeen(s.configPath))
	}
	if strings.Contains(dashboard(t, s), "Eraser is now Delistd") {
		t.Error("card shown again after dismissal")
	}
}

func TestRenameNoticeNotForFreshInstalls(t *testing.T) {
	if (&Server{}).renameNotice() {
		t.Error("no config path: nothing to show")
	}
	empty := newTestServer(t, &config.Config{})
	empty.configPath = smokeServer(t).configPath + ".other"
	if empty.renameNotice() {
		t.Error("no profile yet (setup not done): nothing to show")
	}
	// dismissing without a config path must not panic or write anywhere
	if rec := post(t, &Server{}, "/renamed/dismiss", nil); rec.Code == 0 {
		t.Error("no response")
	}
}
