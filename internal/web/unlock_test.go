package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/drumandbytes/eraser/internal/config"
)

// protectedServer is a server whose config holds a passphrase-protected mail
// password, started without the passphrase (serve with no terminal).
func protectedServer(t *testing.T) *Server {
	t.Helper()
	cfg := testConfig("default")
	cfg.Email = config.EmailConfig{From: "a@b.co", SMTP: config.SMTPConfig{Host: "h", Port: 587, Username: "u", Password: "smtp-pw"}}
	s := newTestServer(t, cfg)
	s.configPath = t.TempDir() + "/config.yaml"
	config.SetPassphrase("")
	t.Cleanup(func() { config.SetPassphrase("") })

	protected := cfg.Clone()
	if err := protected.SetPassphraseProtection("correct horse"); err != nil {
		t.Fatal(err)
	}
	if err := s.saveConfig(protected); err != nil {
		t.Fatal(err)
	}
	loaded, err := config.Load(s.configPath)
	if err != nil {
		t.Fatal(err)
	}
	s.config.Store(loaded)
	config.SetPassphrase("")
	return s
}

func TestLockedServerRedirectsToUnlock(t *testing.T) {
	s := protectedServer(t)
	for _, path := range []string{"/", "/brokers", "/settings"} {
		rec := httptest.NewRecorder()
		s.setupRouter().ServeHTTP(rec, loopbackRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/unlock" {
			t.Errorf("GET %s while locked = %d %q, want redirect to /unlock", path, rec.Code, rec.Header().Get("Location"))
		}
	}
	rec := httptest.NewRecorder()
	req := loopbackRequest(http.MethodPost, "/api/send/spokeo", nil)
	s.setupRouter().ServeHTTP(rec, req)
	if rec.Code != http.StatusLocked {
		t.Errorf("POST while locked = %d, want 423", rec.Code)
	}
	rec = httptest.NewRecorder()
	s.setupRouter().ServeHTTP(rec, loopbackRequest(http.MethodGet, "/unlock", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Unlock Eraser") {
		t.Errorf("unlock page = %d", rec.Code)
	}
}

func TestUnlockWithWrongThenRightPassphrase(t *testing.T) {
	s := protectedServer(t)
	rec := post(t, s, "/unlock", url.Values{"passphrase": {"nope-nope"}})
	if rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), "Wrong passphrase") || config.PassphraseUnlocked() {
		t.Fatalf("wrong passphrase = %d unlocked=%v", rec.Code, config.PassphraseUnlocked())
	}
	rec = post(t, s, "/unlock", url.Values{"passphrase": {"correct horse"}})
	if rec.Code != http.StatusSeeOther || !config.PassphraseUnlocked() {
		t.Fatalf("right passphrase = %d unlocked=%v", rec.Code, config.PassphraseUnlocked())
	}
	if pw, err := config.Reveal(s.getConfig().Email.SMTP.Password); err != nil || pw != "smtp-pw" {
		t.Errorf("Reveal after unlock = %q, %v", pw, err)
	}
	// Once unlocked the unlock page just goes home.
	rec = httptest.NewRecorder()
	s.setupRouter().ServeHTTP(rec, loopbackRequest(http.MethodGet, "/unlock", nil))
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/" {
		t.Errorf("unlock when already unlocked = %d %q", rec.Code, rec.Header().Get("Location"))
	}
}

func TestSettingsPassphraseSetChangeClear(t *testing.T) {
	cfg := testConfig("default")
	cfg.Email = config.EmailConfig{From: "a@b.co", SMTP: config.SMTPConfig{Host: "h", Port: 587, Username: "u", Password: "smtp-pw"}}
	s := newTestServer(t, cfg)
	s.configPath = t.TempDir() + "/config.yaml"
	if err := s.saveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	config.SetPassphrase("")
	t.Cleanup(func() { config.SetPassphrase("") })

	body := func(rec *httptest.ResponseRecorder) string { return rec.Body.String() }
	rec := post(t, s, "/settings/passphrase", url.Values{"action": {"set"}, "new": {"short"}, "confirm": {"short"}})
	if !strings.Contains(body(rec), "at least 8") {
		t.Errorf("short: %s", body(rec))
	}
	rec = post(t, s, "/settings/passphrase", url.Values{"action": {"set"}, "new": {"long enough 1"}, "confirm": {"different 2"}})
	if !strings.Contains(body(rec), "match") {
		t.Errorf("mismatch: %s", body(rec))
	}
	rec = post(t, s, "/settings/passphrase", url.Values{"action": {"set"}, "new": {"long enough 1"}, "confirm": {"long enough 1"}})
	if !strings.Contains(body(rec), "Passphrase set") || !s.getConfig().PassphraseProtected {
		t.Fatalf("set: %s", body(rec))
	}
	raw, _ := os.ReadFile(s.configPath)
	if strings.Contains(string(raw), "smtp-pw") || !strings.Contains(string(raw), "enc:v2:") {
		t.Fatalf("not protected on disk:\n%s", raw)
	}
	if s.getConfig().Email.SMTP.Password == "smtp-pw" {
		t.Error("the live config must hold what is on disk (ciphertext), not the plain password")
	}

	// A wrong current passphrase changes nothing and doesn't lock the app.
	rec = post(t, s, "/settings/passphrase", url.Values{"action": {"clear"}, "current": {"nope nope 1"}})
	if !strings.Contains(body(rec), "current passphrase is wrong") || !s.getConfig().PassphraseProtected || !config.PassphraseUnlocked() {
		t.Errorf("wrong current: %s unlocked=%v", body(rec), config.PassphraseUnlocked())
	}

	rec = post(t, s, "/settings/passphrase", url.Values{"action": {"set"}, "current": {"long enough 1"}, "new": {"another one 3"}, "confirm": {"another one 3"}})
	if !strings.Contains(body(rec), "Passphrase set") {
		t.Fatalf("change: %s", body(rec))
	}
	rec = post(t, s, "/settings/passphrase", url.Values{"action": {"clear"}, "current": {"another one 3"}})
	if !strings.Contains(body(rec), "Passphrase removed") || s.getConfig().PassphraseProtected {
		t.Fatalf("clear: %s", body(rec))
	}
	raw, _ = os.ReadFile(s.configPath)
	if strings.Contains(string(raw), "enc:v2:") || !strings.Contains(string(raw), "enc:v1:") {
		t.Errorf("not back to machine-bound:\n%s", raw)
	}
	if s.getConfig().Email.SMTP.Password != "smtp-pw" {
		t.Errorf("password lost: %q", s.getConfig().Email.SMTP.Password)
	}
	rec = post(t, s, "/settings/passphrase", url.Values{"action": {"clear"}})
	if !strings.Contains(body(rec), "No passphrase is set") {
		t.Errorf("clear when none set: %s", body(rec))
	}
}

func TestUnlockAndPassphraseEndpointsRateLimit(t *testing.T) {
	s := protectedServer(t)
	s.rateLimiter = NewRateLimiter(0, time.Minute)
	if rec := post(t, s, "/unlock", url.Values{"passphrase": {"correct horse"}}); rec.Code != http.StatusTooManyRequests || config.PassphraseUnlocked() {
		t.Errorf("unlock over the limit = %d unlocked=%v", rec.Code, config.PassphraseUnlocked())
	}

	open := newTestServer(t, testConfig("default"))
	open.rateLimiter = NewRateLimiter(0, time.Minute)
	rec := post(t, open, "/settings/passphrase", url.Values{"action": {"set"}, "new": {"long enough 1"}, "confirm": {"long enough 1"}})
	if rec.Code != http.StatusTooManyRequests || !strings.Contains(rec.Body.String(), "Too many attempts") {
		t.Errorf("passphrase over the limit = %d", rec.Code)
	}
}

func TestSettingsPassphraseNeedsConfigAndSurvivesFailures(t *testing.T) {
	none := newTestServer(t, testConfig("default"))
	none.config.Store(nil)
	if rec := post(t, none, "/settings/passphrase", url.Values{"action": {"set"}}); !strings.Contains(rec.Body.String(), "No configuration found") {
		t.Errorf("no config: %s", rec.Body.String())
	}

	// A config path that can't be written: the change is refused, nothing is stored.
	bad := newTestServer(t, testConfig("default"))
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	bad.configPath = filepath.Join(blocker, "sub", "config.yaml") // a directory can't be created under a file
	config.SetPassphrase("")
	t.Cleanup(func() { config.SetPassphrase("") })
	rec := post(t, bad, "/settings/passphrase", url.Values{"action": {"set"}, "new": {"long enough 1"}, "confirm": {"long enough 1"}})
	if !strings.Contains(rec.Body.String(), "Failed to save") || bad.getConfig().PassphraseProtected {
		t.Errorf("unwritable config: %s", rec.Body.String())
	}

	// Changing a passphrase while this process doesn't hold the current one fails cleanly.
	locked := protectedServer(t)
	req := loopbackRequest(http.MethodPost, "/settings/passphrase", strings.NewReader(url.Values{"action": {"clear"}, "current": {"correct horse"}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	locked.handleSettingsPassphrase(w, req)
	if !strings.Contains(w.Body.String(), "change the passphrase") {
		t.Errorf("locked process: code=%d", w.Code)
	}
}

func TestVerifyPassphraseNeedsAReadableConfig(t *testing.T) {
	s := newTestServer(t, testConfig("default"))
	s.configPath = t.TempDir() + "/missing.yaml"
	if err := s.verifyPassphrase("x"); err == nil {
		t.Error("a missing config file can't verify anything")
	}
}

func badFormRequest(path string) *http.Request {
	req := loopbackRequest(http.MethodPost, path, strings.NewReader("passphrase=%zz"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return req
}

func TestUnlockAndPassphraseRejectMalformedForms(t *testing.T) {
	s := protectedServer(t)
	rec := httptest.NewRecorder()
	s.setupRouter().ServeHTTP(rec, badFormRequest("/unlock"))
	if !strings.Contains(rec.Body.String(), "Failed to parse form") || config.PassphraseUnlocked() {
		t.Errorf("unlock with a bad body = %d %.200s", rec.Code, rec.Body.String())
	}

	open := newTestServer(t, testConfig("default"))
	rec = httptest.NewRecorder()
	open.setupRouter().ServeHTTP(rec, badFormRequest("/settings/passphrase"))
	if !strings.Contains(rec.Body.String(), "Failed to parse form") {
		t.Errorf("settings with a bad body = %d", rec.Code)
	}
}

func TestSettingsPassphraseWarnsWhenTheOSScheduleIsInstalled(t *testing.T) {
	s := newTestServer(t, testConfig("default"))
	s.configPath = t.TempDir() + "/config.yaml"
	s.osInstalled = func() bool { return true }
	config.SetPassphrase("")
	t.Cleanup(func() { config.SetPassphrase("") })
	rec := post(t, s, "/settings/passphrase", url.Values{"action": {"set"}, "new": {"long enough 1"}, "confirm": {"long enough 1"}})
	if !strings.Contains(rec.Body.String(), "OS schedule can no longer send") {
		t.Errorf("missing the scheduler warning: %.300s", rec.Body.String())
	}
}
