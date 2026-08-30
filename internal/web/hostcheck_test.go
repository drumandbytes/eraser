package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eraser-privacy/eraser/internal/config"
)

// The Host allowlist is the only thing standing between this server and a DNS
// rebinding attack: a malicious page whose domain re-resolves to 127.0.0.1
// becomes same-origin with the UI in the browser's eyes, so the CSRF layer
// (which is origin-based) waves it through. Such a request still carries the
// attacker's hostname in Host, which is what this middleware pins.
//
// These go through the real router from setupRouter, since the middleware
// only exists there - the other web tests call handlers directly and so never
// see it.
func TestHostCheck(t *testing.T) {
	s := newTestServer(t, testConfig())
	s.port = 8080
	router := s.setupRouter()

	tests := []struct {
		name     string
		host     string
		wantCode int
	}{
		{"localhost with port", "localhost:8080", http.StatusOK},
		{"127.0.0.1 with port", "127.0.0.1:8080", http.StatusOK},
		{"localhost bare", "localhost", http.StatusOK},
		{"127.0.0.1 bare", "127.0.0.1", http.StatusOK},
		{"IPv6 loopback", "[::1]:8080", http.StatusOK},

		// The rebinding case: an attacker-controlled name that resolves to
		// loopback. The connection succeeds; only the Host header gives it away.
		{"rebound attacker domain", "evil.example.com", http.StatusForbidden},
		{"rebound with port", "evil.example.com:8080", http.StatusForbidden},

		// Suffix/prefix tricks. All of these resolve to 127.0.0.1 in the real
		// world, so a HasPrefix/HasSuffix/Contains comparison would admit them.
		{"loopback name as prefix", "localhost.evil.com", http.StatusForbidden},
		{"loopback IP as prefix", "127.0.0.1.evil.com", http.StatusForbidden},
		{"loopback name as suffix", "evil-localhost", http.StatusForbidden},
		{"wildcard DNS service", "127.0.0.1.nip.io", http.StatusForbidden},

		// Go's http.Server rejects HTTP/1.1 without a Host already; allowing
		// empty here would just reopen the bypass for anything that omits it.
		{"empty host", "", http.StatusForbidden},

		// Right names, wrong port - not an origin this server is reachable at.
		{"wrong port", "localhost:9999", http.StatusForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/brokers", nil)
			req.Host = tt.host

			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if rec.Code != tt.wantCode {
				t.Errorf("Host %q: status = %d, want %d", tt.host, rec.Code, tt.wantCode)
			}
		})
	}
}

// A rejected request must not disclose page content - the whole point is that
// the attacker's script can't read the app password off /settings.
func TestHostCheckLeaksNoBody(t *testing.T) {
	s := newTestServer(t, &config.Config{
		Profile: config.Profile{FirstName: "Test", LastName: "User", Email: "test@example.com"},
		Inbox:   config.InboxConfig{Enabled: true, Email: "test@gmail.com", Password: "topsecretapppw"},
	})
	s.port = 8080
	router := s.setupRouter()

	req := httptest.NewRequest(http.MethodGet, "/settings", nil)
	req.Host = "evil.example.com"

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
	if body := rec.Body.String(); strings.Contains(body, "topsecretapppw") {
		t.Error("rejected response leaked the stored app password")
	}
}
