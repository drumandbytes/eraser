package main

import (
	"net"
	"net/http"
	"testing"

	"github.com/drumandbytes/eraser/internal/broker"
)

func stubChecker(mxOK, hostOK, headStatus int, headErr error) *auditChecker {
	return &auditChecker{
		lookupMX: func(domain string) ([]*net.MX, error) {
			if mxOK == 0 {
				return nil, nil
			}
			return []*net.MX{{Host: "mail." + domain}}, nil
		},
		lookupHost: func(domain string) ([]string, error) {
			if hostOK == 0 {
				return nil, nil
			}
			return []string{"1.2.3.4"}, nil
		},
		httpHead: func(url string) (*http.Response, error) {
			if headErr != nil {
				return nil, headErr
			}
			return &http.Response{StatusCode: headStatus, Body: http.NoBody}, nil
		},
	}
}

func TestAuditOneAliveWhenMXAndWebsiteOK(t *testing.T) {
	b := broker.Broker{ID: "spokeo", Email: "privacy@spokeo.com", Website: "https://spokeo.com"}
	checker := stubChecker(1, 0, 200, nil)

	if got := auditOne(b, checker); got != verdictAlive {
		t.Errorf("auditOne() = %q, want %q", got, verdictAlive)
	}
}

func TestAuditOneEmailDeadWhenMXAndHostBothFail(t *testing.T) {
	b := broker.Broker{ID: "spokeo", Email: "privacy@spokeo.com", Website: "https://spokeo.com"}
	checker := stubChecker(0, 0, 200, nil)

	if got := auditOne(b, checker); got != verdictEmailDead {
		t.Errorf("auditOne() = %q, want %q", got, verdictEmailDead)
	}
}

func TestAuditOneEmailAliveViaHostFallbackWhenNoMX(t *testing.T) {
	b := broker.Broker{ID: "spokeo", Email: "privacy@spokeo.com"}
	checker := stubChecker(0, 1, 200, nil)

	if got := auditOne(b, checker); got != verdictAlive {
		t.Errorf("auditOne() = %q, want %q (host lookup should be a valid fallback when MX is absent)", got, verdictAlive)
	}
}

func TestAuditOneWebsiteDeadOnConnectionError(t *testing.T) {
	b := broker.Broker{ID: "spokeo", Email: "privacy@spokeo.com", Website: "https://gone.example"}
	checker := stubChecker(1, 0, 0, errConnRefused)

	if got := auditOne(b, checker); got != verdictWebsiteDead {
		t.Errorf("auditOne() = %q, want %q", got, verdictWebsiteDead)
	}
}

func TestAuditOneWebsiteUnknownOnNon2xxStatus(t *testing.T) {
	b := broker.Broker{ID: "spokeo", Email: "privacy@spokeo.com", Website: "https://spokeo.com"}
	checker := stubChecker(1, 0, 403, nil)

	if got := auditOne(b, checker); got != verdictUnknown {
		t.Errorf("auditOne() = %q, want %q (a 403 shouldn't be treated as dead - many privacy pages block headless requests)", got, verdictUnknown)
	}
}

func TestAuditOneSkippedWhenNoEmailOrWebsite(t *testing.T) {
	b := broker.Broker{ID: "no-contact"}
	checker := stubChecker(1, 1, 200, nil)

	if got := auditOne(b, checker); got != verdictSkipped {
		t.Errorf("auditOne() = %q, want %q", got, verdictSkipped)
	}
}

func TestAuditOneEmailDeadTakesPriorityOverWebsiteDead(t *testing.T) {
	// Both signals point to "gone" - email-dead is the one that should be
	// reported (it's the more actionable, more certain signal).
	b := broker.Broker{ID: "spokeo", Email: "privacy@spokeo.com", Website: "https://spokeo.com"}
	checker := stubChecker(0, 0, 0, errConnRefused)

	if got := auditOne(b, checker); got != verdictEmailDead {
		t.Errorf("auditOne() = %q, want %q", got, verdictEmailDead)
	}
}

func TestAuditOneMalformedEmailIsDead(t *testing.T) {
	b := broker.Broker{ID: "bad-email", Email: "not-an-email"}
	checker := stubChecker(1, 1, 200, nil)

	if got := auditOne(b, checker); got != verdictEmailDead {
		t.Errorf("auditOne() = %q, want %q for an email with no @", got, verdictEmailDead)
	}
}

type connRefusedErr struct{}

func (connRefusedErr) Error() string { return "connection refused" }

var errConnRefused = connRefusedErr{}
