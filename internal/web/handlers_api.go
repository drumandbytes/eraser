package web

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"time"

	"github.com/eraser-privacy/eraser/internal/history"
	"github.com/eraser-privacy/eraser/internal/inbox"
	"github.com/go-chi/chi/v5"
)

// API handlers

func (s *Server) handleAPIBrokers(w http.ResponseWriter, r *http.Request) {
	search := r.URL.Query().Get("search")
	category := r.URL.Query().Get("category")
	region := r.URL.Query().Get("region")
	status := r.URL.Query().Get("status")
	missingEmail := r.URL.Query().Get("missing_email") == "true"

	brokers := s.getBrokersWithStatus(s.activeProfile(r).ID, search, category, region, status, missingEmail)

	// Returns broker list as HTML fragment for HTMX
	s.renderPartial(w, "partials/broker-list.html", map[string]interface{}{
		"Brokers":  brokers,
		"Filtered": len(brokers),
		"Total":    len(s.brokerDB.Brokers),
	})
}

// handleAPIBrokerStatus returns just the status-badge fragment for one
// broker (the desktop badge, plus an out-of-band swap for the mobile one) -
// used by the brokers page during an active send job to refresh only the
// row that just changed, instead of re-fetching and re-rendering the
// entire (700+) broker table on every poll tick. See
// internal/history.Store.GetBrokerStatus for the single-broker-scoped query
// this uses instead of the all-brokers GROUP BY.
func (s *Server) handleAPIBrokerStatus(w http.ResponseWriter, r *http.Request) {
	brokerID := chi.URLParam(r, "brokerID")
	if s.brokerDB.FindByID(brokerID) == nil {
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

func (s *Server) handleAPIInboxScan(w http.ResponseWriter, r *http.Request) {
	// Check if inbox is configured
	cfg := s.getConfig()
	if cfg == nil || !cfg.Inbox.Enabled {
		_, _ = w.Write([]byte(`
			<div class="bg-yellow-100 border border-yellow-400 text-yellow-800 px-4 py-3 rounded">
				<strong>Inbox monitoring not configured.</strong>
				<p class="mt-1 text-sm">Go to <a href="/settings" class="underline">Settings</a> to configure IMAP access.</p>
			</div>
		`))
		return
	}

	// Create inbox monitor
	monitor := inbox.NewMonitor(cfg.Inbox, s.brokerDB.Brokers)

	// Connect to IMAP
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	if err := monitor.Connect(ctx); err != nil {
		_, _ = fmt.Fprintf(w, `
			<div class="bg-red-100 border border-red-400 text-red-700 px-4 py-3 rounded">
				<strong>Failed to connect to inbox:</strong> %s
			</div>
		`, template.HTMLEscapeString(err.Error()))
		return
	}
	defer func() { _ = monitor.Disconnect() }()

	// Fetch emails from last 7 days - check both INBOX and archive folder
	emails, err := monitor.FetchBrokerEmails(ctx, 7)
	if err != nil {
		_, _ = fmt.Fprintf(w, `
			<div class="bg-red-100 border border-red-400 text-red-700 px-4 py-3 rounded">
				<strong>Failed to fetch emails:</strong> %s
			</div>
		`, template.HTMLEscapeString(err.Error()))
		return
	}

	// Also check archive folder if configured
	if cfg.Inbox.ArchiveFolder != "" {
		archiveEmails, err := monitor.FetchBrokerEmailsFromFolder(ctx, cfg.Inbox.ArchiveFolder, 7)
		if err != nil {
			log.Printf("Warning: failed to fetch from archive folder %s: %v", cfg.Inbox.ArchiveFolder, err)
		} else {
			emails = append(emails, archiveEmails...)
		}
	}

	if len(emails) == 0 {
		_, _ = w.Write([]byte(`
			<div class="bg-blue-100 border border-blue-400 text-blue-700 px-4 py-3 rounded">
				<strong>No new broker emails found.</strong>
				<p class="mt-1 text-sm">No emails from known data brokers in the last 7 days.</p>
			</div>
		`))
		return
	}

	// Classify and store each email
	var success, formRequired, confirmRequired, rejected, disclosure, unknown int
	var processedUIDs []uint32 // Track UIDs for archiving
	for _, email := range emails {
		classified := inbox.ClassifyResponse(&email)
		processedUIDs = append(processedUIDs, email.UID)

		// Get body content (prefer plain text, fall back to HTML)
		bodyContent := email.Body
		if bodyContent == "" {
			bodyContent = email.HTMLBody
		}

		// A shared inbox carries replies for every profile's sent requests
		// together, so attribute this reply to whichever profile actually
		// emailed this broker rather than to whatever profile is "active"
		// in the session running this scan.
		profileID := history.DefaultProfileID
		if s.historyStore != nil {
			if resolved, err := s.historyStore.ResolveProfileForBroker(email.BrokerID); err == nil {
				profileID = resolved
			}
		}

		// Store in database
		brokerResp := &history.BrokerResponse{
			ProfileID:    profileID,
			BrokerID:     email.BrokerID,
			BrokerName:   email.BrokerName,
			ResponseType: string(classified.Type),
			EmailFrom:    email.From,
			EmailSubject: email.Subject,
			EmailBody:    bodyContent,
			FormURL:      classified.FormURL,
			ConfirmURL:   classified.ConfirmURL,
			Confidence:   classified.Confidence,
			NeedsReview:  classified.NeedsReview,
			ReceivedAt:   email.ReceivedAt,
		}

		if s.historyStore != nil {
			if err := s.historyStore.AddBrokerResponse(brokerResp); err != nil {
				// Don't let a DB write failure silently vanish while the
				// in-memory counters below still report success - this is
				// exactly what caused digisamroc/eraser#17 (Pipeline page
				// empty despite "Scan Complete!" reporting matches).
				log.Printf("Warning: failed to store broker response for %s: %v", brokerResp.BrokerID, err)
			}
		}

		// Count by type
		switch classified.Type {
		case inbox.ResponseSuccess:
			success++
		case inbox.ResponseFormRequired:
			formRequired++
		case inbox.ResponseConfirmationRequired:
			confirmRequired++
		case inbox.ResponseRejected:
			rejected++
		case inbox.ResponseDisclosure:
			disclosure++
		default:
			unknown++
		}
	}

	// Auto-archive processed emails to the Eraser folder
	var archived int
	if cfg.Inbox.AutoArchive && len(processedUIDs) > 0 {
		if err := monitor.ArchiveEmails(processedUIDs, cfg.Inbox.ArchiveFolder); err != nil {
			log.Printf("Warning: failed to archive emails: %v", err)
		} else {
			archived = len(processedUIDs)
			log.Printf("Archived %d emails to %s folder", archived, cfg.Inbox.ArchiveFolder)
		}
	}

	// Return summary HTML
	_, _ = fmt.Fprintf(w, `
		<div class="bg-green-100 border border-green-400 text-green-800 px-4 py-3 rounded">
			<strong>Scan complete!</strong> Found %d broker emails.
			<div class="mt-2 text-sm grid grid-cols-2 gap-2">
				<div>Success: <span class="font-semibold">%d</span></div>
				<div>Form required: <span class="font-semibold">%d</span></div>
				<div>Confirm required: <span class="font-semibold">%d</span></div>
				<div>Rejected: <span class="font-semibold">%d</span></div>
				<div>Data disclosures: <span class="font-semibold">%d</span></div>
				<div>Unknown: <span class="font-semibold">%d</span></div>
			</div>
			<p class="mt-2 text-sm">
				<a href="/tasks" class="underline font-medium">View pending tasks</a> |
				<a href="/pipeline" class="underline" onclick="window.location.reload()">Refresh page</a>
			</p>
		</div>
	`, len(emails), success, formRequired, confirmRequired, rejected, disclosure, unknown)
}

// handleAPIInboxRescan rescans all emails and reclassifies them with the improved classifier
func (s *Server) handleAPIInboxRescan(w http.ResponseWriter, r *http.Request) {
	// Check if inbox is configured
	cfg := s.getConfig()
	if cfg == nil || !cfg.Inbox.Enabled {
		_, _ = w.Write([]byte(`
			<div class="bg-yellow-100 border border-yellow-400 text-yellow-800 px-4 py-3 rounded">
				<strong>Inbox monitoring not configured.</strong>
				<p class="mt-1 text-sm">Go to <a href="/settings" class="underline">Settings</a> to configure IMAP access.</p>
			</div>
		`))
		return
	}

	// Check if clear flag is set
	clearFirst := r.URL.Query().Get("clear") == "true"
	if clearFirst && s.historyStore != nil {
		if err := s.historyStore.ClearBrokerResponses(); err != nil {
			_, _ = fmt.Fprintf(w, `
				<div class="bg-red-100 border border-red-400 text-red-700 px-4 py-3 rounded">
					<strong>Failed to clear responses:</strong> %s
				</div>
			`, template.HTMLEscapeString(err.Error()))
			return
		}
	}

	// Create inbox monitor
	monitor := inbox.NewMonitor(cfg.Inbox, s.brokerDB.Brokers)

	// Connect to IMAP with longer timeout for full rescan
	ctx, cancel := context.WithTimeout(r.Context(), 180*time.Second)
	defer cancel()

	if err := monitor.Connect(ctx); err != nil {
		_, _ = fmt.Fprintf(w, `
			<div class="bg-red-100 border border-red-400 text-red-700 px-4 py-3 rounded">
				<strong>Failed to connect to inbox:</strong> %s
			</div>
		`, template.HTMLEscapeString(err.Error()))
		return
	}
	defer func() { _ = monitor.Disconnect() }()

	// Fetch emails from last 30 days for full rescan - check both INBOX and archive folder
	emails, err := monitor.FetchBrokerEmails(ctx, 30)
	if err != nil {
		_, _ = fmt.Fprintf(w, `
			<div class="bg-red-100 border border-red-400 text-red-700 px-4 py-3 rounded">
				<strong>Failed to fetch emails:</strong> %s
			</div>
		`, template.HTMLEscapeString(err.Error()))
		return
	}

	// Also check archive folder if configured
	if cfg.Inbox.ArchiveFolder != "" {
		archiveEmails, err := monitor.FetchBrokerEmailsFromFolder(ctx, cfg.Inbox.ArchiveFolder, 30)
		if err != nil {
			log.Printf("Warning: failed to fetch from archive folder %s: %v", cfg.Inbox.ArchiveFolder, err)
		} else {
			emails = append(emails, archiveEmails...)
		}
	}

	if len(emails) == 0 {
		_, _ = w.Write([]byte(`
			<div class="bg-blue-100 border border-blue-400 text-blue-700 px-4 py-3 rounded">
				<strong>No broker emails found.</strong>
				<p class="mt-1 text-sm">No emails from known data brokers in the last 30 days.</p>
			</div>
		`))
		return
	}

	// Classify and store/update each email
	var success, formRequired, confirmRequired, rejected, pending, disclosure, unknown int
	var updated, inserted int
	for _, email := range emails {
		classified := inbox.ClassifyResponse(&email)

		// Get body content (prefer plain text, fall back to HTML)
		bodyContent := email.Body
		if bodyContent == "" {
			bodyContent = email.HTMLBody
		}

		// A shared inbox carries replies for every profile's sent requests
		// together, so attribute this reply to whichever profile actually
		// emailed this broker rather than to whatever profile is "active"
		// in the session running this rescan.
		profileID := history.DefaultProfileID
		if s.historyStore != nil {
			if resolved, err := s.historyStore.ResolveProfileForBroker(email.BrokerID); err == nil {
				profileID = resolved
			}
		}

		// Check if this response already exists
		if s.historyStore != nil {
			existing, _ := s.historyStore.FindBrokerResponseBySubject(profileID, email.BrokerID, email.Subject)
			if existing != nil {
				// Update existing response classification
				err := s.historyStore.UpdateBrokerResponseClassification(
					existing.ID,
					existing.ProfileID,
					string(classified.Type),
					classified.FormURL,
					classified.ConfirmURL,
					classified.Confidence,
					classified.NeedsReview,
				)
				if err == nil {
					updated++
				} else {
					log.Printf("Warning: failed to update broker response classification for %s: %v", email.BrokerID, err)
				}
				// Also update the body if it was empty
				if existing.EmailBody == "" && bodyContent != "" {
					if err := s.historyStore.UpdateBrokerResponseBody(existing.ID, existing.ProfileID, bodyContent); err != nil {
						log.Printf("Warning: failed to update broker response body for %s: %v", email.BrokerID, err)
					}
				}
			} else {
				// Insert new response
				brokerResp := &history.BrokerResponse{
					ProfileID:    profileID,
					BrokerID:     email.BrokerID,
					BrokerName:   email.BrokerName,
					ResponseType: string(classified.Type),
					EmailFrom:    email.From,
					EmailSubject: email.Subject,
					EmailBody:    bodyContent,
					FormURL:      classified.FormURL,
					ConfirmURL:   classified.ConfirmURL,
					Confidence:   classified.Confidence,
					NeedsReview:  classified.NeedsReview,
					ReceivedAt:   email.ReceivedAt,
				}
				if err := s.historyStore.AddBrokerResponse(brokerResp); err == nil {
					inserted++
				} else {
					log.Printf("Warning: failed to store broker response for %s: %v", email.BrokerID, err)
				}
			}
		}

		// Count by type
		switch classified.Type {
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
		case inbox.ResponseDisclosure:
			disclosure++
		default:
			unknown++
		}
	}

	// Return summary HTML
	_, _ = fmt.Fprintf(w, `
		<div class="bg-green-100 border border-green-400 text-green-800 px-4 py-3 rounded">
			<strong>Rescan complete!</strong> Processed %d broker emails.
			<div class="mt-2 text-sm grid grid-cols-2 gap-2">
				<div>Updated: <span class="font-semibold">%d</span></div>
				<div>New: <span class="font-semibold">%d</span></div>
			</div>
			<div class="mt-2 text-sm grid grid-cols-3 gap-2">
				<div>Success: <span class="font-semibold">%d</span></div>
				<div>Form required: <span class="font-semibold">%d</span></div>
				<div>Confirm required: <span class="font-semibold">%d</span></div>
				<div>Pending: <span class="font-semibold">%d</span></div>
				<div>Rejected: <span class="font-semibold">%d</span></div>
				<div>Data disclosures: <span class="font-semibold">%d</span></div>
				<div>Unknown: <span class="font-semibold">%d</span></div>
			</div>
			<p class="mt-2 text-sm">
				<a href="/tasks" class="underline font-medium">View action items</a> |
				<a href="/pipeline" class="underline" onclick="window.location.reload()">Refresh page</a>
			</p>
		</div>
	`, len(emails), updated, inserted, success, formRequired, confirmRequired, pending, rejected, disclosure, unknown)
}

// handleAPIReclassify reclassifies all existing database records using subject-only patterns
func (s *Server) handleAPIReclassify(w http.ResponseWriter, r *http.Request) {
	if s.historyStore == nil {
		_, _ = w.Write([]byte(`
			<div class="bg-red-100 border border-red-400 text-red-700 px-4 py-3 rounded">
				<strong>Database not available.</strong>
			</div>
		`))
		return
	}

	// Get all broker responses
	responses, err := s.historyStore.GetAllBrokerResponses()
	if err != nil {
		_, _ = fmt.Fprintf(w, `
			<div class="bg-red-100 border border-red-400 text-red-700 px-4 py-3 rounded">
				<strong>Failed to get responses:</strong> %s
			</div>
		`, template.HTMLEscapeString(err.Error()))
		return
	}

	if len(responses) == 0 {
		_, _ = w.Write([]byte(`
			<div class="bg-blue-100 border border-blue-400 text-blue-700 px-4 py-3 rounded">
				<strong>No responses to reclassify.</strong>
			</div>
		`))
		return
	}

	// Check how many records are missing email bodies
	var missingBodies int
	for _, resp := range responses {
		if resp.EmailBody == "" {
			missingBodies++
		}
	}

	// If there are records missing bodies, try to fetch from IMAP
	cfg := s.getConfig()
	var bodiesUpdated int
	if missingBodies > 0 && cfg != nil && cfg.Inbox.Server != "" && s.brokerDB != nil {
		log.Printf("Found %d records missing email bodies, fetching from IMAP...", missingBodies)

		monitor := inbox.NewMonitor(cfg.Inbox, s.brokerDB.Brokers)

		ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
		defer cancel()

		if err := monitor.Connect(ctx); err != nil {
			log.Printf("Warning: failed to connect to IMAP for body fetch: %v", err)
		} else {
			defer func() { _ = monitor.Disconnect() }()

			// Fetch emails from both INBOX and archive folder
			var allEmails []inbox.Email

			// Fetch from INBOX
			emails, err := monitor.FetchRecentEmails(ctx, 30) // 30 days
			if err != nil {
				log.Printf("Warning: failed to fetch from INBOX: %v", err)
			} else {
				allEmails = append(allEmails, emails...)
			}

			// Also fetch from archive folder if configured
			if cfg.Inbox.ArchiveFolder != "" {
				archiveEmails, err := monitor.FetchBrokerEmailsFromFolder(ctx, cfg.Inbox.ArchiveFolder, 30)
				if err != nil {
					log.Printf("Warning: failed to fetch from archive folder: %v", err)
				} else {
					allEmails = append(allEmails, archiveEmails...)
				}
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

			// Update database records with missing bodies
			for _, resp := range responses {
				if resp.EmailBody != "" {
					continue // Already has body
				}
				key := resp.BrokerID + "|" + resp.EmailSubject
				if body, ok := emailBodies[key]; ok {
					err := s.historyStore.UpdateBrokerResponseBody(resp.ID, resp.ProfileID, body)
					if err == nil {
						bodiesUpdated++
						// Update the in-memory response too for reclassification
						for i := range responses {
							if responses[i].ID == resp.ID {
								responses[i].EmailBody = body
								break
							}
						}
					}
				}
			}
			log.Printf("Updated %d records with email bodies from IMAP", bodiesUpdated)
		}
	}

	// Reclassify each response - use full classifier if body available, otherwise subject-only
	var updated, unchanged int
	var pending, rejected, success, formRequired, confirmRequired, disclosure, unknown int

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
		case inbox.ResponseDisclosure:
			disclosure++
		default:
			unknown++
		}
	}

	// Return summary HTML
	_, _ = fmt.Fprintf(w, `
		<div class="bg-green-100 border border-green-400 text-green-800 px-4 py-3 rounded">
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
				<div>Data disclosures: <span class="font-semibold">%d</span></div>
				<div>Unknown: <span class="font-semibold">%d</span></div>
			</div>
			<p class="mt-2 text-sm">
				<a href="/tasks" class="underline font-medium">View action items</a> |
				<a href="/pipeline" class="underline" onclick="window.location.reload()">Refresh page</a>
			</p>
		</div>
	`, len(responses), updated, unchanged, pending, rejected, success, formRequired, confirmRequired, disclosure, unknown)
}
