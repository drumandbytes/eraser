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

	"github.com/go-chi/chi/v5"
	"github.com/eraser-privacy/eraser/internal/broker"
	"github.com/eraser-privacy/eraser/internal/config"
	"github.com/eraser-privacy/eraser/internal/email"
	"github.com/eraser-privacy/eraser/internal/history"
)

// checkPendingJob checks for an incomplete job from a previous session and resumes it
func (s *Server) checkPendingJob() {
	state, err := s.jobPersistence.Load()
	if err != nil {
		log.Printf("Warning: failed to load pending job: %v", err)
		return
	}

	if state == nil || len(state.RemainingBrokers) == 0 {
		return // No pending job
	}

	fmt.Printf("\nFound incomplete send job: %d of %d brokers remaining\n", len(state.RemainingBrokers), state.Total)
	fmt.Printf("Already sent: %d, failed: %d\n", state.Sent, state.Failed)

	// Auto-resume the job
	go s.resumePendingJob(state)
}

// resumePendingJob resumes processing of an incomplete job
func (s *Server) resumePendingJob(state *PersistentJobState) {
	// Wait a moment for the server to fully start
	time.Sleep(2 * time.Second)

	cfg := s.getConfig()
	if cfg == nil || cfg.Email.Provider == "" {
		log.Printf("Cannot resume job: email not configured")
		_ = s.jobPersistence.Clear()
		return
	}

	// Create email sender
	sender, err := email.NewSender(cfg.Email)
	if err != nil {
		log.Printf("Cannot resume job: failed to create email sender: %v", err)
		_ = s.jobPersistence.Clear()
		return
	}

	// Build broker list from remaining IDs
	brokerMap := make(map[string]broker.Broker)
	for _, b := range s.brokerDB.Brokers {
		brokerMap[b.ID] = b
	}

	var toSend []BrokerWithStatus
	for _, id := range state.RemainingBrokers {
		if b, ok := brokerMap[id]; ok {
			toSend = append(toSend, BrokerWithStatus{Broker: b, Status: "never"})
		}
	}

	if len(toSend) == 0 {
		log.Printf("No valid brokers remaining in pending job")
		_ = s.jobPersistence.Clear()
		return
	}

	// Create a new job to continue processing, preserving the profile the
	// original job was scoped to. state.ProfileID is empty for a job
	// persisted before multi-profile support existed - normalizes to the
	// same "default" every other pre-migration record falls back to.
	profileID := state.ProfileID
	if profileID == "" {
		profileID = config.DefaultProfileID
	}
	job := s.jobManager.Create(state.Total, profileID)
	job.Update(state.Sent, state.Failed, "")

	fmt.Printf("Resuming send job: %d brokers remaining...\n", len(toSend))

	// Process remaining brokers
	s.processSendJob(job, toSend, sender)
}

func (s *Server) handleAPISendOne(w http.ResponseWriter, r *http.Request) {
	// Rate limiting - prevent abuse of email sending
	if !s.rateLimiter.Allow("send") {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`<span class="text-yellow-600">Rate limit exceeded. Please wait a moment before sending more emails.</span>`))
		return
	}

	brokerID := chi.URLParam(r, "brokerID")

	br := s.brokerDB.FindByID(brokerID)
	if br == nil {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`<span class="text-red-600">Broker not found</span>`))
		return
	}

	cfg := s.getConfig()
	if cfg == nil || cfg.Email.Provider == "" {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`<span class="text-red-600">Email not configured. <a href="/setup" class="underline">Configure now</a></span>`))
		return
	}

	if br.Email == "" {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`<span class="text-amber-600">No email on file - needs manual follow-up (check for an opt-out form/portal)</span>`))
		return
	}

	// Create email sender
	sender, err := email.NewSender(cfg.Email)
	if err != nil {
		_, _ = fmt.Fprintf(w, `<span class="text-red-600">Error: %s</span>`, template.HTMLEscapeString(err.Error()))
		return
	}

	// Generate email content using template engine. Use the user's
	// configured template (gdpr/ccpa/generic) - this used to be hardcoded
	// to "generic", which meant every web-UI send cited generic privacy law
	// language instead of GDPR Article 17 regardless of what init/settings
	// configured. config.Load guarantees Options.Template is never empty.
	activeProfile := s.activeProfile(r)
	tmplName := cfg.Options.Template
	rendered, err := s.tmplEngine.Render(tmplName, activeProfile.Profile, *br)
	if err != nil {
		_, _ = fmt.Fprintf(w, `<span class="text-red-600">Template error: %s</span>`, template.HTMLEscapeString(err.Error()))
		return
	}

	msg := email.Message{
		To:      br.Email,
		From:    cfg.Email.From,
		Subject: rendered.Subject,
		Body:    rendered.Body,
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	result := sender.Send(ctx, msg)

	// Record in history
	record := &history.Record{
		ProfileID:  activeProfile.ID,
		BrokerID:   br.ID,
		BrokerName: br.Name,
		Email:      br.Email,
		Template:   tmplName,
		SentAt:     time.Now(),
	}

	if result.Success {
		record.Status = history.StatusSent
		record.MessageID = result.MessageID
	} else {
		record.Status = history.StatusFailed
		if result.Error != nil {
			record.Error = result.Error.Error()
		}
	}

	if s.historyStore != nil {
		_ = s.historyStore.Add(record)
	}

	if result.Success {
		_, _ = w.Write([]byte(`<span class="px-2 inline-flex text-xs leading-5 font-semibold rounded-full bg-green-100 text-green-800">Sent</span>`))
	} else {
		errMsg := "Unknown error"
		if result.Error != nil {
			errMsg = result.Error.Error()
		}
		_, _ = fmt.Fprintf(w, `<span class="text-red-600" title="%s">Failed</span>`, template.HTMLEscapeString(errMsg))
	}
}

func (s *Server) handleAPISendAll(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Rate limiting - prevent abuse of bulk email sending
	if !s.rateLimiter.Allow("send-all") {
		w.WriteHeader(http.StatusTooManyRequests)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Rate limit exceeded. Please wait before sending another batch."})
		return
	}

	activeProfile := s.activeProfile(r)

	// Check if a job is already running for this profile
	if activeJob := s.jobManager.GetActive(activeProfile.ID); activeJob != nil {
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"error":  "A send job is already in progress",
			"job_id": activeJob.ID,
		})
		return
	}

	cfg := s.getConfig()
	if cfg == nil || cfg.Email.Provider == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Email not configured. Please configure email settings first."})
		return
	}

	// Get filter parameters from form
	limitFormBody(w, r)
	search := r.FormValue("search")
	category := r.FormValue("category")
	region := r.FormValue("region")
	status := r.FormValue("status")

	// If no status filter specified, default to pending (never sent)
	if status == "" {
		status = "pending"
	}

	// Bulk send never targets missing-email brokers - there's nowhere to send.
	toSend := s.getBrokersWithStatus(activeProfile.ID, search, category, region, status, false)

	if len(toSend) == 0 {
		noneMsg := "No pending brokers to send to."
		if status == "failed" {
			noneMsg = "No failed brokers to retry."
		} else if status != "" && status != "pending" {
			noneMsg = fmt.Sprintf("No brokers matching status %q to send to.", status)
		}
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": noneMsg})
		return
	}

	// Create email sender (validate config before starting job)
	sender, err := email.NewSender(cfg.Email)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	// Create a new job
	job := s.jobManager.Create(len(toSend), activeProfile.ID)

	// Extract broker IDs for persistence
	brokerIDs := make([]string, len(toSend))
	for i, b := range toSend {
		brokerIDs[i] = b.ID
	}

	// Save initial job state
	jobState := &PersistentJobState{
		ID:               job.ID,
		ProfileID:        activeProfile.ID,
		Status:           job.GetStatus(),
		Sent:             0,
		Failed:           0,
		Total:            len(toSend),
		StartedAt:        job.StartedAt,
		RemainingBrokers: brokerIDs,
		Search:           search,
		Category:         category,
		Region:           region,
		StatusFilter:     status,
	}
	if err := s.jobPersistence.Save(jobState); err != nil {
		log.Printf("Warning: failed to save job state: %v", err)
	}

	// Start background goroutine to process emails
	go s.processSendJob(job, toSend, sender)

	// Return job ID immediately
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"job_id": job.ID,
		"total":  len(toSend),
	})
}

// defaultDailyLimit is used only if the config's daily_send_limit is unset -
// config.Load already fills this in normally, so this is just a safety net.
const defaultDailyLimit = 250 // Gmail/SMTP: stay well under 500/day

// processSendJob runs in a background goroutine to send emails
func (s *Server) processSendJob(job *Job, toSend []BrokerWithStatus, sender email.Sender) {
	sent := 0
	failed := 0

	cfg := s.getConfig()

	// This runs in a background goroutine with no *http.Request to read the
	// active-profile cookie from, so the profile is fixed to whatever it
	// was when the job was created (job.ProfileID) - correct even if the
	// user switches the web UI's active profile mid-send.
	activeProfile, err := cfg.GetProfile(job.ProfileID)
	if err != nil {
		// job.ProfileID no longer exists in config (e.g. edited out between
		// job creation and now) - fall back to whatever GetProfile("")
		// resolves to rather than crash the whole job.
		if profiles := cfg.GetProfiles(); len(profiles) > 0 {
			activeProfile = profiles[0]
		}
	}

	rateLimitMs := cfg.Options.RateLimitMs
	if rateLimitMs == 0 {
		rateLimitMs = 2000 // Default 2 second delay
	}

	// Respect the same daily_send_limit the CLI `send` command uses, so the
	// web UI and CLI don't disagree about how many emails/day is safe.
	dailyLimit := cfg.Options.DailySendLimit
	if dailyLimit == 0 {
		dailyLimit = defaultDailyLimit
	}
	job.SetDailyLimit(dailyLimit)

	// Track remaining brokers for persistence
	remaining := make([]string, len(toSend))
	for i, b := range toSend {
		remaining[i] = b.ID
	}

	for i, b := range toSend {
		// Check if job was cancelled
		if job.IsCancelled() {
			break
		}

		// Check daily limit
		if sent >= dailyLimit {
			job.Pause(sent, fmt.Sprintf("Daily limit of %d emails reached. Remaining %d brokers will be sent when you restart tomorrow.", dailyLimit, len(remaining)))
			s.saveJobProgress(job, sent, failed, remaining)
			log.Printf("Job paused: daily limit of %d reached, %d remaining", dailyLimit, len(remaining))
			return
		}

		// Update current broker
		job.Update(sent, failed, b.Name)

		// Generate email using the user's configured template (see the
		// same fix/comment in handleAPISendOne above)
		rendered, err := s.tmplEngine.Render(cfg.Options.Template, activeProfile.Profile, b.Broker)
		if err != nil {
			failed++
			job.Update(sent, failed, b.Name)
			// Remove from remaining even on failure
			remaining = remaining[1:]
			s.saveJobProgress(job, sent, failed, remaining)
			continue
		}

		msg := email.Message{
			To:      b.Email,
			From:    cfg.Email.From,
			Subject: rendered.Subject,
			Body:    rendered.Body,
		}

		// Use job's context with timeout for cancellation support
		ctx, cancel := context.WithTimeout(job.Context(), 30*time.Second)
		result := sender.Send(ctx, msg)
		cancel()

		// Record in history
		record := &history.Record{
			ProfileID:  activeProfile.ID,
			BrokerID:   b.ID,
			BrokerName: b.Name,
			Email:      b.Email,
			Template:   cfg.Options.Template,
			SentAt:     time.Now(),
		}

		if result.Success {
			record.Status = history.StatusSent
			record.MessageID = result.MessageID
			sent++
			job.ResetAuthFailures() // Reset on success
		} else {
			record.Status = history.StatusFailed
			errMsg := ""
			if result.Error != nil {
				errMsg = result.Error.Error()
				record.Error = errMsg
			}
			failed++

			// Check for auth failures and stop if too many consecutive
			if strings.Contains(strings.ToLower(errMsg), "auth") {
				if job.RecordAuthFailure() {
					// Stop job due to auth errors
					if s.historyStore != nil {
						_ = s.historyStore.Add(record)
					}
					remaining = remaining[1:]
					s.saveJobProgress(job, sent, failed, remaining)
					job.StopWithError("auth", "Stopped due to repeated authentication failures. Your email provider may have rate-limited or blocked your account. Please check your email settings and try again later.")
					log.Printf("Job stopped: repeated auth failures after %d sent, %d failed", sent, failed)
					return
				}
			}
		}

		if s.historyStore != nil {
			_ = s.historyStore.Add(record)
		}

		// Update job progress
		job.Update(sent, failed, b.Name)

		// Remove processed broker from remaining and save state
		remaining = remaining[1:]
		s.saveJobProgress(job, sent, failed, remaining)

		// Rate limit delay (skip on last item)
		if i < len(toSend)-1 && !job.IsCancelled() {
			time.Sleep(time.Duration(rateLimitMs) * time.Millisecond)
		}
	}

	// Mark job as complete and clear persisted state
	job.Complete()
	if err := s.jobPersistence.Clear(); err != nil {
		log.Printf("Warning: failed to clear job state: %v", err)
	}
}

// saveJobProgress saves the current job progress to disk
func (s *Server) saveJobProgress(job *Job, sent, failed int, remaining []string) {
	state := &PersistentJobState{
		ID:               job.ID,
		ProfileID:        job.ProfileID,
		Status:           job.GetStatus(),
		Sent:             sent,
		Failed:           failed,
		Total:            job.Total,
		StartedAt:        job.StartedAt,
		RemainingBrokers: remaining,
	}
	if err := s.jobPersistence.Save(state); err != nil {
		log.Printf("Warning: failed to save job progress: %v", err)
	}
}

// handleAPIJobActive returns the currently running job (if any)
func (s *Server) handleAPIJobActive(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	job := s.jobManager.GetActive(s.activeProfile(r).ID)
	if job == nil {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"job": nil})
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{"job": job.ToJSON()})
}

// handleAPIJobStatus returns the status of a specific job
func (s *Server) handleAPIJobStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	jobID := chi.URLParam(r, "jobID")
	job := s.jobManager.Get(jobID)
	if job != nil && job.ProfileID != s.activeProfile(r).ID {
		job = nil
	}

	if job == nil {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "job not found"})
		return
	}

	_ = json.NewEncoder(w).Encode(job.ToJSON())
}

// handleAPIJobCancel cancels a running job
func (s *Server) handleAPIJobCancel(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	jobID := chi.URLParam(r, "jobID")
	job := s.jobManager.Get(jobID)
	if job != nil && job.ProfileID != s.activeProfile(r).ID {
		job = nil
	}

	if job == nil {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "job not found"})
		return
	}

	job.Cancel()
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "cancelled"})
}
