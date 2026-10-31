package web

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestUpgradeNoticeShowsUntilDismissed(t *testing.T) {
	s := smokeServer(t)
	if s.upgradeNotice() != nil {
		t.Fatal("no backup, no notice")
	}
	if err := os.WriteFile(s.configPath+".1x.bak", []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	s.setupRouter().ServeHTTP(rec, loopbackRequest(http.MethodGet, "/settings", nil))
	if !strings.Contains(rec.Body.String(), "Config upgraded") {
		t.Fatalf("notice missing from /settings (%d)", rec.Code)
	}

	rec = post(t, s, "/upgrade-notice/dismiss", nil)
	if rec.Code != http.StatusSeeOther || s.upgradeNotice() != nil {
		t.Fatalf("dismiss = %d, notice still shown: %v", rec.Code, s.upgradeNotice() != nil)
	}
	rec = httptest.NewRecorder()
	s.setupRouter().ServeHTTP(rec, loopbackRequest(http.MethodGet, "/settings", nil))
	if strings.Contains(rec.Body.String(), "Config upgraded") {
		t.Error("notice shown again after dismissal")
	}
	noPath := newTestServer(t, testConfig("default"))
	noPath.handleDismissUpgradeNotice(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/", nil))
	if noPath.upgradeNotice() != nil {
		t.Error("server without a config path has nothing to show")
	}
}
