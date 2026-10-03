package web

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/drumandbytes/eraser/internal/config"
	"github.com/drumandbytes/eraser/internal/history"
	"github.com/drumandbytes/eraser/internal/inbox"
	"github.com/go-chi/chi/v5"
)

// API handlers

func (s *Server) handleAPIBrokers(w http.ResponseWriter, r *http.Request) {
	search := r.URL.Query().Get("search")
	category := r.URL.Query().Get("category")
	region := r.URL.Query().Get("region")
	status := r.URL.Query().Get("status")
	missingEmail := r.URL.Query().Get("missing_email") == "true"
	showExcluded := r.URL.Query().Get("show_excluded") == "true"

	brokers := s.getBrokersWithStatus(s.activeProfile(r).ID, search, category, region, status, nil, nil, missingEmail, showExcluded)

	s.renderPartial(w, "partials/broker-list.html", map[string]interface{}{
		"Brokers":      brokers,
		"Filtered":     len(brokers),
		"Total":        len(s.brokers().Brokers),
		"ShowExcluded": showExcluded,
	})
}

// handleAPIBrokerStatus returns one broker's status badge (plus the mobile OOB
// swap), so an active send refreshes one row instead of the 700+ row table.
func (s *Server) handleAPIBrokerStatus(w http.ResponseWriter, r *http.Request) {
	brokerID := chi.URLParam(r, "brokerID")
	if s.brokers().FindByID(brokerID) == nil {
		http.Error(w, "Broker not found", http.StatusNotFound)
		return
	}

	statusStr := "never"
	if s.historyStore != nil {
		if bs, err := s.historyStore.GetBrokerStatus(s.activeProfile(r).ID, brokerID); err == nil && bs.TotalSent > 0 {
			statusStr = string(bs.Status)
		}
	}

	s.renderPartial(w, "partials/broker-status-badge.html", map[string]interface{}{
		"ID":     brokerID,
		"Status": statusStr,
	})
}

// handleAPIExcludeBroker adds a broker to Options.ExcludedBrokers so it's
// skipped by every send path (bulk send, per-row send, and job resume on
// startup - see getBrokersWithStatus), and re-renders just that row's
// actions fragment so the brokers page can toggle it without a full reload.
func (s *Server) handleAPIExcludeBroker(w http.ResponseWriter, r *http.Request) {
	s.setBrokerExcluded(w, r, true)
}

// handleAPIIncludeBroker undoes handleAPIExcludeBroker.
func (s *Server) handleAPIIncludeBroker(w http.ResponseWriter, r *http.Request) {
	s.setBrokerExcluded(w, r, false)
}

func (s *Server) setBrokerExcluded(w http.ResponseWriter, r *http.Request, exclude bool) {
	brokerID := chi.URLParam(r, "brokerID")
	b := s.brokers().FindByID(brokerID)
	if b == nil {
		http.Error(w, "Broker not found", http.StatusNotFound)
		return
	}

	// Load-copy-mutate-store, same pattern as handleSettingsInbox - s.config
	// is read concurrently by other handlers and background send-job
	// goroutines, so the pointer from getConfig() must never be mutated in
	// place.
	cfg := s.getConfig()
	if cfg == nil {
		cfg = &config.Config{}
	}
	newCfg := *cfg

	id := strings.ToLower(b.ID)
	var updated []string
	found := false
	for _, e := range newCfg.Options.ExcludedBrokers {
		if strings.ToLower(e) == id {
			found = true
			if exclude {
				updated = append(updated, e) // already excluded, keep as-is
			}
			continue // drop it when including
		}
		updated = append(updated, e)
	}
	if exclude && !found {
		updated = append(updated, id)
	}
	newCfg.Options.ExcludedBrokers = updated

	if err := config.Save(s.configPath, &newCfg); err != nil {
		http.Error(w, "Failed to save configuration: "+err.Error(), http.StatusInternalServerError)
		return
	}
	s.config.Store(&newCfg)

	manualMode := false
	if c := s.getConfig(); c != nil {
		manualMode = c.IsManualSend()
	}
	s.renderPartial(w, "partials/broker-actions.html", map[string]interface{}{
		"ID":         b.ID,
		"Email":      b.Email,
		"OptOutURL":  b.OptOutURL,
		"Excluded":   exclude,
		"ManualMode": manualMode,
	})
}

func (s *Server) handleAPIDeleteFailed(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if s.historyStore == nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Database not available"})
		return
	}

	deleted, err := s.historyStore.DeleteByStatus(s.activeProfile(r).ID, history.StatusFailed)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"deleted": deleted,
		"message": fmt.Sprintf("Deleted %d failed records", deleted),
	})
}

// handleAPIDeleteAllHistory backs Settings > Danger Zone > "Clear All
// History". It only clears send history (removal_requests) - broker
// database, config, and inbox-classified responses are untouched.
func (s *Server) handleAPIDeleteAllHistory(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if s.historyStore == nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Database not available"})
		return
	}

	deleted, err := s.historyStore.DeleteAllHistory(s.activeProfile(r).ID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"deleted": deleted,
		"message": fmt.Sprintf("Deleted %d history record(s)", deleted),
	})
}

func (s *Server) handleAPIResponses(w http.ResponseWriter, r *http.Request) {
	responseType := r.URL.Query().Get("type")
	needsReview := r.URL.Query().Get("needs_review") == "true"

	var responses []history.BrokerResponse
	if s.historyStore != nil {
		responses, _ = s.historyStore.GetBrokerResponses(s.activeProfile(r).ID, responseType, needsReview, 50)
	}

	s.renderPartial(w, "partials/response-list.html", map[string]interface{}{
		"Responses": responses,
	})
}

func (s *Server) handleAPIResponseReviewed(w http.ResponseWriter, r *http.Request) {
	var responseID int64
	if _, err := fmt.Sscanf(chi.URLParam(r, "responseID"), "%d", &responseID); err != nil {
		http.Error(w, "Response not found", http.StatusNotFound)
		return
	}
	if s.historyStore == nil {
		http.Error(w, "Database not available", http.StatusInternalServerError)
		return
	}
	if err := s.historyStore.MarkBrokerResponseReviewed(responseID, s.activeProfile(r).ID); err != nil {
		http.Error(w, "Response not found", http.StatusNotFound)
		return
	}
	if r.Header.Get("HX-Request") == "true" {
		w.WriteHeader(http.StatusOK)
		return
	}
	http.Redirect(w, r, "/tasks", http.StatusFound)
}

func (s *Server) handleAPIInboxScan(w http.ResponseWriter, r *http.Request) {
	s.scanInbox(w, r, inbox.ScanOptions{Days: 7, IncludeArchive: true}, 60*time.Second)
}

// handleAPIInboxRescan re-reads 30 days and reclassifies replies already
// stored; ?clear=true drops every stored reply first.
func (s *Server) handleAPIInboxRescan(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("clear") == "true" && s.historyStore != nil && s.inboxConfigured(r) {
		if err := s.historyStore.ClearBrokerResponses(); err != nil {
			writeScanAlert(w, "error", "Failed to clear responses:", err.Error())
			return
		}
	}
	s.scanInbox(w, r, inbox.ScanOptions{Days: 30, IncludeArchive: true, Reclassify: true}, 180*time.Second)
}

func (s *Server) inboxConfigured(r *http.Request) bool {
	cfg := s.getConfig()
	return cfg != nil && cfg.InboxForProfile(s.activeProfile(r)).Enabled
}

// scanInbox scans the active profile's own inbox (its mail.inbox override,
// or the shared inbox: block) - `eraser monitor` and automated cycles cover
// every configured inbox.
func (s *Server) scanInbox(w http.ResponseWriter, r *http.Request, opt inbox.ScanOptions, timeout time.Duration) {
	if !s.inboxConfigured(r) {
		_, _ = w.Write([]byte(`<div class="alert alert-warning"><strong>Inbox monitoring not configured.</strong>
			Go to <a href="/settings" class="underline">Settings</a> to configure IMAP access.</div>`))
		return
	}
	if s.historyStore == nil {
		writeScanAlert(w, "error", "Database not available.", "")
		return
	}
	inboxCfg := s.getConfig().InboxForProfile(s.activeProfile(r))
	monitor := inbox.NewMonitor(inboxCfg, s.brokers().Brokers)

	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()
	if err := monitor.Connect(ctx); err != nil {
		writeScanAlert(w, "error", "Failed to connect to inbox:", err.Error())
		return
	}
	defer func() { _ = monitor.Disconnect() }()

	res, err := monitor.ScanAndStore(ctx, s.historyStore, opt)
	if err != nil {
		writeScanAlert(w, "error", "Failed to fetch emails:", err.Error())
		return
	}
	if res.Summary.Total == 0 {
		writeScanAlert(w, "info", "No broker emails found.", fmt.Sprintf("No emails from known data brokers in the last %d days.", opt.Days))
		return
	}

	sum := res.Summary
	updated := ""
	if opt.Reclassify {
		updated = fmt.Sprintf(`<div>Updated: <span class="font-semibold">%d</span></div>`, res.Updated)
	}
	_, _ = fmt.Fprintf(w, `
		<div class="alert alert-success">
			<strong>Scan complete!</strong> Found %d broker emails.
			<div class="mt-2 text-sm grid grid-cols-2 gap-2">
				<div>New: <span class="font-semibold">%d</span></div>
				%s
			</div>
			<div class="mt-2 text-sm grid grid-cols-3 gap-2">
				<div>Success: <span class="font-semibold">%d</span></div>
				<div>Form required: <span class="font-semibold">%d</span></div>
				<div>Confirm required: <span class="font-semibold">%d</span></div>
				<div>Pending: <span class="font-semibold">%d</span></div>
				<div>Rejected: <span class="font-semibold">%d</span></div>
				<div>Unknown: <span class="font-semibold">%d</span></div>
			</div>
			<p class="mt-2 text-sm">
				<a href="/tasks" class="underline font-medium">View action items</a> |
				<a href="/pipeline" class="underline" onclick="window.location.reload()">Refresh page</a>
			</p>
		</div>
	`, sum.Total, len(res.New), updated, sum.Success, sum.FormRequired, sum.ConfirmRequired, sum.Pending, sum.Rejected, sum.Unknown)
}

// writeScanAlert writes an .alert-<kind> banner (layout.html); detail is escaped.
func writeScanAlert(w http.ResponseWriter, kind, title, detail string) {
	_, _ = fmt.Fprintf(w, `<div class="alert alert-%s"><strong>%s</strong> %s</div>`, kind, title, template.HTMLEscapeString(detail))
}

// handleAPIReclassify reclassifies all existing database records using subject-only patterns
func (s *Server) handleAPIReclassify(w http.ResponseWriter, r *http.Request) {
	if s.historyStore == nil {
		writeScanAlert(w, "error", "Database not available.", "")
		return
	}

	responses, err := s.historyStore.GetAllBrokerResponses()
	if err != nil {
		writeScanAlert(w, "error", "Failed to get responses:", err.Error())
		return
	}

	if len(responses) == 0 {
		writeScanAlert(w, "info", "No responses to reclassify.", "")
		return
	}

	// Check how many records are missing email bodies
	var missingBodies int
	for _, resp := range responses {
		if resp.EmailBody == "" {
			missingBodies++
		}
	}

	// backfill bodies from every configured inbox: responses span all profiles
	cfg := s.getConfig()
	var bodiesUpdated int
	var inboxes []config.InboxConfig
	if cfg != nil {
		inboxes = cfg.ConfiguredInboxes()
	}
	if missingBodies > 0 && len(inboxes) > 0 && s.brokers() != nil {
		log.Printf("Found %d records missing email bodies, fetching from IMAP...", missingBodies)

		// Fetch emails from both INBOX and archive folder, across every
		// configured inbox.
		var allEmails []inbox.Email

		for _, inboxCfg := range inboxes {
			monitor := inbox.NewMonitor(inboxCfg, s.brokers().Brokers)

			ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
			if err := monitor.Connect(ctx); err != nil {
				log.Printf("Warning: failed to connect to IMAP for body fetch: %v", err)
				cancel()
				continue
			}

			emails, err := monitor.FetchBrokerEmails(ctx, 30)
			if err != nil {
				log.Printf("Warning: failed to fetch from INBOX: %v", err)
			} else {
				allEmails = append(allEmails, emails...)
			}

			// Also fetch from archive folder if configured
			if inboxCfg.ArchiveFolder != "" {
				archiveEmails, err := monitor.FetchBrokerEmailsFromFolder(ctx, inboxCfg.ArchiveFolder, 30)
				if err != nil {
					log.Printf("Warning: failed to fetch from archive folder: %v", err)
				} else {
					allEmails = append(allEmails, archiveEmails...)
				}
			}

			_ = monitor.Disconnect()
			cancel()
		}

		log.Printf("Fetched %d emails from IMAP", len(allEmails))

		// Build lookup map: key = "broker_id|subject" -> email body
		emailBodies := make(map[string]string)
		for _, email := range allEmails {
			if email.BrokerID == "" {
				continue // Not matched to a broker
			}
			key := email.BrokerID + "|" + email.Subject
			body := email.Body
			if body == "" {
				body = email.HTMLBody
			}
			if body != "" {
				emailBodies[key] = body
			}
		}

		for i := range responses {
			resp := &responses[i]
			if resp.EmailBody != "" {
				continue // Already has body
			}
			key := resp.BrokerID + "|" + resp.EmailSubject
			if body, ok := emailBodies[key]; ok {
				if err := s.historyStore.UpdateBrokerResponseBody(resp.ID, resp.ProfileID, body); err == nil {
					bodiesUpdated++
					resp.EmailBody = body // in-memory too, for the reclassification below
				}
			}
		}
		log.Printf("Updated %d records with email bodies from IMAP", bodiesUpdated)
	}

	// Reclassify each response - use full classifier if body available, otherwise subject-only
	var updated, unchanged int
	var pending, rejected, success, formRequired, confirmRequired, unknown int

	for _, resp := range responses {
		var newType inbox.ResponseType
		var confidence float64
		var needsReview bool
		var formURL, confirmURL string

		if resp.EmailBody != "" {
			// Use full classifier with body
			email := &inbox.Email{
				From:    resp.EmailFrom,
				Subject: resp.EmailSubject,
				Body:    resp.EmailBody,
			}
			classified := inbox.ClassifyResponse(email)
			newType = classified.Type
			confidence = classified.Confidence
			needsReview = classified.NeedsReview
			formURL = classified.FormURL
			confirmURL = classified.ConfirmURL
		} else {
			// Fall back to subject-only classification
			newType, confidence, needsReview = inbox.ClassifyBySubjectOnly(resp.EmailSubject)
			formURL = resp.FormURL
			confirmURL = resp.ConfirmURL
		}

		// Only update if classification changed or was unknown
		if string(newType) != resp.ResponseType || (resp.ResponseType == "unknown" && newType != inbox.ResponseUnknown) {
			err := s.historyStore.UpdateBrokerResponseClassification(
				resp.ID,
				resp.ProfileID,
				string(newType),
				formURL,
				confirmURL,
				confidence,
				needsReview,
			)
			if err == nil {
				updated++
			}
		} else {
			unchanged++
		}

		// Count by final type
		switch newType {
		case inbox.ResponseSuccess:
			success++
		case inbox.ResponseFormRequired:
			formRequired++
		case inbox.ResponseConfirmationRequired:
			confirmRequired++
		case inbox.ResponseRejected:
			rejected++
		case inbox.ResponsePending:
			pending++
		default:
			unknown++
		}
	}

	_, _ = fmt.Fprintf(w, `
		<div class="alert alert-success">
			<strong>Reclassification complete!</strong> Processed %d records.
			<div class="mt-2 text-sm grid grid-cols-2 gap-2">
				<div>Updated: <span class="font-semibold">%d</span></div>
				<div>Unchanged: <span class="font-semibold">%d</span></div>
			</div>
			<div class="mt-2 text-sm grid grid-cols-3 gap-2">
				<div>Pending: <span class="font-semibold">%d</span></div>
				<div>Rejected: <span class="font-semibold">%d</span></div>
				<div>Success: <span class="font-semibold">%d</span></div>
				<div>Form required: <span class="font-semibold">%d</span></div>
				<div>Confirm required: <span class="font-semibold">%d</span></div>
				<div>Unknown: <span class="font-semibold">%d</span></div>
			</div>
			<p class="mt-2 text-sm">
				<a href="/tasks" class="underline font-medium">View action items</a> |
				<a href="/pipeline" class="underline" onclick="window.location.reload()">Refresh page</a>
			</p>
		</div>
	`, len(responses), updated, unchanged, pending, rejected, success, formRequired, confirmRequired, unknown)
}
