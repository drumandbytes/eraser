package browser

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// Every redirect hop is re-validated: a hop to a disallowed domain is rejected
// before any request, a redirect within allowed domains still works.
func TestClickConfirmationLink_RejectsRedirectToDisallowedDomain(t *testing.T) {
	// evil.test doesn't resolve on purpose: CheckRedirect must reject it
	// before any dial
	allowed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://evil.test/landing", http.StatusFound)
	}))
	defer allowed.Close()

	allowedHost := hostOnly(t, allowed.URL)
	handler := NewConfirmationHandler([]string{allowedHost})

	// Count every dial the client's transport attempts. If redirect
	// validation is working, there should be exactly one dial (the initial
	// request to the allowed server) and no second dial toward evil.test.
	var dialCount int32
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			atomic.AddInt32(&dialCount, 1)
			return (&net.Dialer{}).DialContext(ctx, network, addr)
		},
	}
	handler.client.Transport = transport

	result, err := handler.ClickConfirmationLink(allowed.URL+"/confirm", true)
	if err == nil {
		t.Fatal("expected an error for a redirect to a disallowed domain, got nil")
	}
	if !strings.Contains(err.Error(), "disallowed domain") {
		t.Errorf("error message doesn't look like a redirect-validation failure: %v", err)
	}
	if got := atomic.LoadInt32(&dialCount); got != 1 {
		t.Errorf("transport dialed %d times; want exactly 1 (only the initial request to the allowed server) -- the redirect to evil.test must be blocked before it's dialed", got)
	}
	if result.Success {
		t.Error("result.Success = true for a blocked redirect chain")
	}
}

func TestClickConfirmationLink_AllowsRedirectToAllowedDomain(t *testing.T) {
	final := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("your removal request has been successfully confirmed"))
	}))
	defer final.Close()

	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/confirm" {
			http.Redirect(w, r, final.URL+"/done", http.StatusFound)
			return
		}
	}))
	defer first.Close()

	firstHost := hostOnly(t, first.URL)
	finalHost := hostOnly(t, final.URL)

	// both hops are 127.0.0.1 (ports are stripped), so one entry covers them
	domains := []string{firstHost}
	if finalHost != firstHost {
		domains = append(domains, finalHost)
	}
	handler := NewConfirmationHandler(domains)

	result, err := handler.ClickConfirmationLink(first.URL+"/confirm", true)
	if err != nil {
		t.Fatalf("unexpected error following an allowed redirect: %v", err)
	}
	if !result.Success {
		t.Errorf("expected Success=true, got result=%+v", result)
	}
	if result.FinalURL != final.URL+"/done" {
		t.Errorf("FinalURL = %q, want %q", result.FinalURL, final.URL+"/done")
	}
}

// isSuccessResponse is pure text/status matching - the redirect tests above
// only exercise its "success" branch via a real HTTP round-trip; these check
// the failure-pattern and ambiguous-response branches directly, without
// needing a server.
func TestIsSuccessResponse(t *testing.T) {
	h := NewConfirmationHandler(nil)

	tests := []struct {
		name       string
		statusCode int
		body       string
		want       bool
	}{
		{"200 with success phrase", 200, "Your request has been successfully processed", true},
		{"200 with 'confirmed'", 200, "Your email is confirmed", true},
		{"200 with 'unsubscribed'", 200, "You have been unsubscribed", true},
		{"200 with no recognizable phrase", 200, "<html><body>OK</body></html>", true},
		{"200 but body says link expired", 200, "Sorry, this link expired yesterday", false},
		// "already confirmed" is unreachable: the "confirmed" success pattern
		// matches first. Documents current behavior.
		{"200 but body says already confirmed (shadowed by 'confirmed' success match)", 200, "This request was already confirmed", true},
		{"200 but body says link invalid", 200, "Sorry, link invalid or already used", false},
		{"200 but body says failed", 200, "Something failed while processing", false},
		{"404 status", 404, "Page not found", false},
		{"500 status", 500, "Internal Server Error", false},
		{"300 redirect status alone", 300, "Moved", false},
		{"199 below success range", 199, "success", false},
		// Success phrase present but so is a failure phrase - failure
		// patterns are checked after success patterns and both loops run
		// independently, so whichever phrase appears wins by loop order:
		// success is checked first and returns immediately.
		{"success phrase wins when both present", 200, "Successfully received, but could not verify identity", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := h.isSuccessResponse(tt.statusCode, tt.body); got != tt.want {
				t.Errorf("isSuccessResponse(%d, %q) = %v, want %v", tt.statusCode, tt.body, got, tt.want)
			}
		})
	}
}

func TestExtractConfirmationStatus(t *testing.T) {
	h := NewConfirmationHandler(nil)

	tests := []struct {
		name   string
		result *ConfirmationResult
		want   string
	}{
		{"success short-circuits regardless of body", &ConfirmationResult{Success: true, ResponseBody: "this link expired"}, "Confirmation successful"},
		{"expired", &ConfirmationResult{Success: false, ResponseBody: "Sorry, this link has expired"}, "Link expired"},
		{"already processed", &ConfirmationResult{Success: false, ResponseBody: "This was already confirmed"}, "Already confirmed/processed"},
		{"invalid link", &ConfirmationResult{Success: false, ResponseBody: "The token is invalid"}, "Invalid link"},
		{"404 status with no matching phrase", &ConfirmationResult{Success: false, StatusCode: 404, ResponseBody: "not found"}, "Link not found (404)"},
		{"5xx status with no matching phrase", &ConfirmationResult{Success: false, StatusCode: 502, ResponseBody: "bad gateway"}, "Server error"},
		{"nothing matches", &ConfirmationResult{Success: false, StatusCode: 200, ResponseBody: "hello world"}, "Unknown status"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := h.ExtractConfirmationStatus(tt.result); got != tt.want {
				t.Errorf("ExtractConfirmationStatus(%+v) = %q, want %q", tt.result, got, tt.want)
			}
		})
	}
}

// hostOnly returns an httptest URL's bare host: allowlist entries are stored
// verbatim, so host:port would never match.
func hostOnly(t *testing.T, rawURL string) string {
	t.Helper()
	host := strings.TrimPrefix(rawURL, "http://")
	host = strings.TrimPrefix(host, "https://")
	if idx := strings.Index(host, ":"); idx != -1 {
		host = host[:idx]
	}
	if idx := strings.Index(host, "/"); idx != -1 {
		host = host[:idx]
	}
	return host
}
