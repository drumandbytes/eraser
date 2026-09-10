package main

import (
	"net"
	"net/http"
	"strings"
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

// The distinction that keeps the scheduled audit honest: a connection failure is
// only evidence of death when the hostname has stopped resolving. If DNS still
// answers, we are far likelier to have been refused for running in a datacenter.
// The first scheduled run called 73 brokers dead this way against 1 real one.
// RFC 7505: a lone "." MX means the domain accepts no mail. Go surfaces it as a
// normal record, so a naive len(mxs) > 0 reads it as a working contact.
func TestAuditOneEmailDeadOnNullMX(t *testing.T) {
	b := broker.Broker{ID: "nomail", Email: "privacy@nomail.example", Website: "https://nomail.example"}
	checker := stubChecker(1, 0, 200, nil)
	checker.lookupMX = func(domain string) ([]*net.MX, error) {
		return []*net.MX{{Host: ".", Pref: 0}}, nil
	}

	if got := auditOne(b, checker); got != verdictEmailDead {
		t.Errorf("auditOne() = %q, want %q (a null MX is an explicit refusal of mail)", got, verdictEmailDead)
	}
}

func TestAuditOneWebsiteUnknownWhenHostStillResolves(t *testing.T) {
	b := broker.Broker{ID: "veromi", Email: "privacy@veromi.net", Website: "https://www.veromi.net"}
	checker := stubChecker(1, 1, 0, errConnRefused)

	if got := auditOne(b, checker); got != verdictUnknown {
		t.Errorf("auditOne() = %q, want %q (a resolving host that refuses us is inconclusive, not dead)", got, verdictUnknown)
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

func TestApplyAuditFixOnlyClearsEmailDead(t *testing.T) {
	db := &broker.BrokerDatabase{Brokers: []broker.Broker{
		{ID: "dead", Email: "privacy@dead.example", Notes: "old"},
		{ID: "alive", Email: "privacy@alive.example"},
		{ID: "site-dead", Email: "privacy@sitedead.example", Website: "https://sitedead.example"},
	}}
	results := []auditResult{
		{broker: db.Brokers[0], verdict: verdictEmailDead},
		{broker: db.Brokers[1], verdict: verdictAlive},
		{broker: db.Brokers[2], verdict: verdictWebsiteDead},
	}

	if n := applyAuditFix(db, results); n != 1 {
		t.Fatalf("applyAuditFix cleared %d, want 1", n)
	}
	if db.FindByID("dead").Email != "" {
		t.Error("email-dead broker should have its email cleared")
	}
	if !strings.Contains(db.FindByID("dead").Notes, "old") || !strings.Contains(db.FindByID("dead").Notes, "undeliverable") {
		t.Errorf("expected a dated note appended to the existing one, got %q", db.FindByID("dead").Notes)
	}
	if db.FindByID("alive").Email == "" || db.FindByID("site-dead").Email == "" {
		t.Error("only email-dead entries should be touched")
	}
}

type connRefusedErr struct{}

func (connRefusedErr) Error() string { return "connection refused" }

var errConnRefused = connRefusedErr{}
