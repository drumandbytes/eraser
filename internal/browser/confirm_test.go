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

// Regression coverage for the redirect-hop re-validation added to
// ClickConfirmationLink's http.Client.CheckRedirect callback. Before this
// fix, only the *initial* confirmation URL was checked against the broker
// domain allowlist; a broker site with an open redirect (or a compromised
// first hop) could carry an identifying confirmation token to an arbitrary
// third-party domain across the redirect chain. These tests confirm the
// second hop is rejected before any request to it is made, and that a
// legitimate redirect within the allowed domains still succeeds.
func TestClickConfirmationLink_RejectsRedirectToDisallowedDomain(t *testing.T) {
	// The allowed server redirects to a domain that isn't in the allowlist.
	// "evil.test" deliberately doesn't resolve to anything -- the point is
	// that CheckRedirect must reject it *before* net/http ever tries to
	// dial it, based purely on domain re-validation, not because the host
	// happens to be unreachable.
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

	// Both hops resolve to 127.0.0.1 but on different ports, and
	// matchesAllowedDomain strips the port -- so without listing both
	// loopback "hosts" explicitly this would collapse to one entry. Since
	// httptest servers all share the 127.0.0.1 hostname, list it once; this
	// still exercises the CheckRedirect path re-validating each hop.
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

// hostOnly returns the host:port-stripped-of-port... actually just the bare
// host (matchesAllowedDomain strips the port itself, but NewConfirmationHandler
// stores domains verbatim, so passing host:port would never match an
// incoming request's port-stripped host). This extracts just the hostname
// portion of an httptest.Server's URL for use as an allowlist entry.
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
