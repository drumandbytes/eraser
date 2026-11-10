package web

import (
	"strings"
	"testing"
)

// The nav-link rule (display: inline-flex) outranks .hidden, so without its own
// override the job pill is visible on every page even when nothing is sending.
func TestJobPillStaysHiddenWithoutAJob(t *testing.T) {
	body := dashboard(t, smokeServer(t))
	if !strings.Contains(body, `id="global-job-indicator" href="/brokers" class="hidden`) {
		t.Fatal("job indicator should render hidden")
	}
	if !strings.Contains(body, ".site-nav-inner a.hidden { display: none; }") {
		t.Error("the hidden override for nav links is missing: the pill would show with no job running")
	}
}
