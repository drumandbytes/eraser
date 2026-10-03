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
	"github.com/drumandbytes/eraser/internal/email"
	"github.com/drumandbytes/eraser/internal/history"
	"github.com/go-chi/chi/v5"
)

func (s *Server) handleAPISendOne(w http.ResponseWriter, r *http.Request) {
	// Rate limiting - prevent abuse of email sending
	if !s.rateLimiter.Allow("send") {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`<span class="text-warning">Rate limit exceeded. Please wait a moment before sending more emails.</span>`))
		return
	}

	brokerID := chi.URLParam(r, "brokerID")

	br := s.brokerDB.FindByID(brokerID)
	if br == nil {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`<span class="text-error">Broker not found</span>`))
		return
	}

	cfg := s.getConfig()
	if cfg == nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`<span class="text-error">Email not configured. <a href="/setup" class="underline">Configure now</a></span>`))
		return
	}

	activeProfile := s.activeProfile(r)
	emailCfg := cfg.EmailForProfile(activeProfile)
	if !emailCfg.Configured() {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`<span class="text-error">Email not configured. <a href="/setup" class="underline">Configure now</a></span>`))
		return
	}

	if cfg.Options.DryRun {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`<span class="text-warning">Web sending is disabled while options.dry_run is true.</span>`))
		return
	}
	if br.Email == "" {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`<span class="text-warning">No email on file - needs manual follow-up (check for an opt-out form/portal)</span>`))
		return
	}

	sender, err := email.NewSender(emailCfg)
	if err != nil {
		_, _ = fmt.Fprintf(w, `<span class="text-error">Error: %s</span>`, template.HTMLEscapeString(err.Error()))
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	record, err := email.SendRemoval(ctx, sender, s.tmplEngine, cfg.Options.Template, activeProfile, emailCfg.From, *br)
	if err != nil {
		_, _ = fmt.Fprintf(w, `<span class="text-error">Template error: %s</span>`, template.HTMLEscapeString(err.Error()))
		return
	}
	s.recordSend(record)

	if record.Status == history.StatusSent {
		_, _ = w.Write([]byte(`<span class="badge badge-success">Sent</span>`))
	} else {
		_, _ = fmt.Fprintf(w, `<span class="text-error" title="%s">Failed</span>`, template.HTMLEscapeString(record.Error))
	}
}

// recordSend stores a send attempt, logging (not failing on) a write error.
func (s *Server) recordSend(record *history.Record) {
	if s.historyStore == nil {
		return
	}
	if err := s.historyStore.Add(record); err != nil {
		log.Printf("Warning: failed to record send to %s in history: %v", record.BrokerID, err)
	}
}

func splitBrokerIDs(value string) []string {
	return strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == '\n' || r == ' ' || r == '\t'
	})
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

	if activeJob := s.jobManager.GetActive(activeProfile.ID); activeJob != nil {
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"error":  "A send job is already in progress",
			"job_id": activeJob.ID,
		})
		return
	}

	cfg := s.getConfig()
	if cfg == nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Email not configured. Please configure email settings first."})
		return
	}
	emailCfg := cfg.EmailForProfile(activeProfile)
	if !emailCfg.Configured() {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Email not configured. Please configure email settings first."})
		return
	}
	if cfg.Options.DryRun {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Web sending is disabled while options.dry_run is true."})
		return
	}

	limitFormBody(w, r)
	search := r.FormValue("search")
	category := r.FormValue("category")
	region := r.FormValue("region")
	status := strings.ToLower(strings.TrimSpace(r.FormValue("status")))
	includeIDs := splitBrokerIDs(r.FormValue("broker_ids"))
	excludeIDs := splitBrokerIDs(r.FormValue("exclude_ids"))
	if status == "" {
		status = "eligible"
	}
	if status != "eligible" && status != "never" && status != "failed" && status != "all" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Status must be eligible, never, failed, or all."})
		return
	}
	if unknown := s.brokerDB.UnknownIDs(includeIDs); len(unknown) > 0 {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Unknown broker IDs: " + strings.Join(unknown, ", ")})
		return
	}

	// Bulk send never targets missing-email or excluded brokers - there's
	// nowhere to send, and exclusion means "don't send to this one".
	toSend := s.getBrokersWithStatus(activeProfile.ID, search, category, region, status, includeIDs, excludeIDs, false, false)
	filtered := toSend[:0:0]
	for _, b := range toSend {
		if strings.TrimSpace(b.Email) != "" {
			filtered = append(filtered, b)
		}
	}
	toSend = filtered
	history.SortBySendPriority(toSend, func(b BrokerWithStatus) time.Time { return b.lastSentAt })

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
	sender, err := email.NewSender(emailCfg)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	// GetActive above is only a fast-fail; CreateIfNoActive re-checks under the lock
	// An automatic cycle (in-app, CLI or OS job) sends to the same brokers;
	// running both at once could email a broker twice. Checked under cycleMu
	// so the in-app scheduler can't start one between this check and the
	// job existing.
	s.cycleMu.Lock()
	if s.cycleRunning || s.cycleInProgress() {
		s.cycleMu.Unlock()
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "An automatic run is sending right now. Try again once it finishes."})
		return
	}
	job, created := s.jobManager.CreateIfNoActive(len(toSend), activeProfile.ID)
	s.cycleMu.Unlock()
	if !created {
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"error":  "A send job is already in progress",
			"job_id": job.ID,
		})
		return
	}

	go s.processSendJob(job, toSend, sender)

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"job_id": job.ID,
		"total":  len(toSend),
	})
}

// defaultDailyLimit is used only if the config's daily_send_limit is unset -
// config.Load already fills this in normally, so this is just a safety net.
const defaultDailyLimit = 250 // stay well under typical provider caps (Gmail ~500/day)

// effectiveDailyLimit is the configured limit if positive, else the default.
// Shared by the banner and processSendJob so they can't disagree.
func effectiveDailyLimit(cfg *config.Config) int {
	if cfg != nil && cfg.Options.DailySendLimit > 0 {
		return cfg.Options.DailySendLimit
	}
	return defaultDailyLimit
}

// processSendJob runs in a background goroutine to send emails
func (s *Server) processSendJob(job *Job, toSend []BrokerWithStatus, sender *email.SMTPSender) {
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
	dailyLimit := effectiveDailyLimit(cfg)
	job.SetDailyLimit(dailyLimit)

	// count the real rolling 24h (like the CLI's CountSentSince), or the limit
	// resets on every resume or second "Send all" the same day
	alreadySentToday := 0
	if s.historyStore != nil {
		if n, err := s.historyStore.CountSentSince(activeProfile.ID, time.Now().Add(-24*time.Hour)); err == nil {
			alreadySentToday = n
		} else {
			log.Printf("Warning: failed to check daily send count, limit will only cover this run: %v", err)
		}
	}

	for i, b := range toSend {
		if job.IsCancelled() {
			break
		}

		// Check daily limit
		if alreadySentToday+sent >= dailyLimit {
			next := "Click Send all again tomorrow to send the rest, or turn on automation in Settings."
			if s.inAppScheduling() || s.osInstalled() {
				next = "Automation will send the rest once the limit frees up."
			}
			left := len(toSend) - i
			job.Pause(sent, fmt.Sprintf("Daily limit of %d emails reached. %d brokers remaining. %s", dailyLimit, left, next))
			log.Printf("Job paused: daily limit of %d reached (%d already sent today, %d this run), %d remaining", dailyLimit, alreadySentToday, sent, left)
			return
		}

		job.Update(sent, failed, b.Name, b.ID)

		ctx, cancel := context.WithTimeout(job.Context(), 30*time.Second)
		record, err := email.SendRemoval(ctx, sender, s.tmplEngine, cfg.Options.Template, activeProfile, cfg.EmailForProfile(activeProfile).From, b.Broker)
		cancel()
		if err != nil {
			failed++
			job.Update(sent, failed, b.Name, b.ID)
			continue
		}
		s.recordSend(record)

		if record.Status == history.StatusSent {
			sent++
			job.ResetAuthFailures()
		} else {
			failed++
			if strings.Contains(strings.ToLower(record.Error), "auth") && job.RecordAuthFailure() {
				job.StopWithError("auth", "Stopped due to repeated authentication failures. Your email provider may have rate-limited or blocked your account. Please check your email settings and try again later.")
				log.Printf("Job stopped: repeated auth failures after %d sent, %d failed", sent, failed)
				return
			}
		}

		job.Update(sent, failed, b.Name, b.ID)

		// Rate limit delay (skip on last item)
		if i < len(toSend)-1 && !job.IsCancelled() {
			time.Sleep(time.Duration(rateLimitMs) * time.Millisecond)
		}
	}

	job.Complete()
}

// handleAPIJobActive returns the currently running job (if any)
func (s *Server) handleAPIJobActive(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	job := s.jobManager.GetActive(s.activeProfile(r).ID)
	if job == nil {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"job": nil})
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]any{"job": job})
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

	_ = json.NewEncoder(w).Encode(job)
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
