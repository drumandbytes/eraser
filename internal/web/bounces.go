package web

import (
	"context"
	"fmt"
	"html/template"
	"net/http"
	"strings"
	"time"

	"github.com/drumandbytes/eraser/internal/bounces"
)

// handleAPIBounces is `eraser cleanup-bounces` (dry run): scan the inbox for
// undeliverable mail and list the broker addresses that look dead.
func (s *Server) handleAPIBounces(w http.ResponseWriter, r *http.Request) {
	rep, ok := s.scanBounces(w, r)
	if !ok {
		return
	}
	if rep.Total == 0 {
		writeScanAlert(w, "info", "No bounced emails found.", "")
		return
	}
	dead := rep.Dead()
	var b strings.Builder
	fmt.Fprintf(&b, `<div class="alert alert-%s"><strong>Found %d bounced email(s).</strong>`, map[bool]string{true: "warning", false: "info"}[len(dead) > 0], rep.Total)
	if len(dead) > 0 {
		b.WriteString(`<ul class="mt-2 text-sm space-y-1">`)
		for _, it := range dead {
			fmt.Fprintf(&b, `<li><span class="font-semibold">%s</span> - %s (%s), %s</li>`,
				template.HTMLEscapeString(it.Email), template.HTMLEscapeString(it.Broker.Name), template.HTMLEscapeString(it.Broker.ID), it.ReceivedAt.Format("2006-01-02"))
		}
		b.WriteString(`</ul>`)
		fmt.Fprintf(&b, `<p class="mt-3 text-sm">Clearing keeps the broker and its other details and notes the old address; use Brokers, "no email" to find them afterwards.</p>
			<button class="btn btn-primary mt-2" hx-post="/api/inbox/bounces/clear" hx-target="#scan-result" hx-swap="innerHTML">Clear %d dead address(es)</button>`, len(dead))
	} else {
		b.WriteString(` <span class="text-sm">None of them need action.</span>`)
	}
	skipped := 0
	for _, it := range rep.Items {
		if it.Kind != bounces.Dead {
			skipped++
		}
	}
	if skipped > 0 {
		fmt.Fprintf(&b, `<p class="mt-2 text-xs text-muted">%d other bounce(s) were ignored: no address could be read, the address isn't in the broker list, or Eraser never emailed it.</p>`, skipped)
	}
	b.WriteString(`</div>`)
	_, _ = fmt.Fprint(w, b.String())
}

// handleAPIBouncesClear is `eraser cleanup-bounces --remove`. It re-scans
// rather than trusting a posted list, then clears every dead address.
func (s *Server) handleAPIBouncesClear(w http.ResponseWriter, r *http.Request) {
	rep, ok := s.scanBounces(w, r)
	if !ok {
		return
	}
	dead := rep.Dead()
	if len(dead) == 0 {
		writeScanAlert(w, "info", "No broker email addresses need to be cleared.", "")
		return
	}
	cleared, err := bounces.Clear(s.BrokerOverride, dead)
	if err != nil {
		writeScanAlert(w, "error", "Couldn't clear the addresses:", err.Error())
		return
	}
	if err := s.reloadBrokers(); err != nil {
		writeScanAlert(w, "error", "Cleared, but reloading the broker list failed:", err.Error())
		return
	}
	_, _ = fmt.Fprintf(w, `<div class="alert alert-success"><strong>Cleared %d broker email address(es).</strong> The brokers are kept and ready for a new address.</div>`, len(cleared))
}

func (s *Server) scanBounces(w http.ResponseWriter, r *http.Request) (*bounces.Report, bool) {
	cfg := s.getConfig()
	if cfg == nil || len(cfg.ConfiguredInboxes()) == 0 {
		writeScanAlert(w, "warning", "Inbox monitoring is not configured.", "Set it up in Settings first.")
		return nil, false
	}
	if s.historyStore == nil {
		writeScanAlert(w, "error", "Database not available.", "")
		return nil, false
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	rep, err := bounces.Scan(ctx, cfg.ConfiguredInboxes(), s.brokers(), bounces.EverSent(cfg, s.historyStore), 30)
	if err != nil {
		writeScanAlert(w, "error", "Bounce check failed:", err.Error())
		return nil, false
	}
	return rep, true
}
