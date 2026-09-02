package web

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"

	"github.com/eraser-privacy/eraser/internal/history"
	"github.com/go-chi/chi/v5"
)

// Handler implementations

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	// Check if config exists, redirect to setup if not
	cfg := s.getConfig()
	if cfg == nil || cfg.Profile.FirstName == "" && len(cfg.Profiles) == 0 {
		http.Redirect(w, r, "/setup", http.StatusFound)
		return
	}

	active := s.activeProfile(r)
	data := map[string]interface{}{
		"Title":         "Dashboard",
		"Profile":       active.Profile,
		"BrokerCount":   len(s.brokerDB.Brokers),
		"RecentHistory": s.getRecentHistory(active.ID, 10),
		"Stats":         s.getStats(active.ID),
		"PipelineStats": s.getPipelineStats(active.ID),
	}

	s.renderWithCSRF(w, r, "dashboard.html", data)
}

func (s *Server) handleBrokers(w http.ResponseWriter, r *http.Request) {
	search := r.URL.Query().Get("search")
	category := r.URL.Query().Get("category")
	region := r.URL.Query().Get("region")
	status := r.URL.Query().Get("status")
	missingEmail := r.URL.Query().Get("missing_email") == "true"
	showExcluded := r.URL.Query().Get("show_excluded") == "true"

	brokers := s.getBrokersWithStatus(s.activeProfile(r).ID, search, category, region, status, missingEmail, showExcluded)

	dailyLimit := effectiveDailyLimit(s.getConfig())

	data := map[string]interface{}{
		"Title":        "Data Brokers",
		"Brokers":      brokers,
		"Categories":   s.getUniqueCategories(),
		"Regions":      s.getUniqueRegions(),
		"Search":       search,
		"Category":     category,
		"Region":       region,
		"Status":       status,
		"MissingEmail": missingEmail,
		"ShowExcluded": showExcluded,
		"Total":        len(s.brokerDB.Brokers),
		"Filtered":     len(brokers),
		"DailyLimit":   dailyLimit,
		"ManualMode":   s.getConfig() != nil && s.getConfig().IsManualSend(),
	}
	s.renderWithCSRF(w, r, "brokers.html", data)
}

func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request) {
	statusFilter := r.URL.Query().Get("status")
	allHistory := s.getRecentHistory(s.activeProfile(r).ID, 1000)

	// Filter by status if specified
	var filteredHistory []history.Record
	if statusFilter == "sent" || statusFilter == "failed" {
		for _, h := range allHistory {
			if string(h.Status) == statusFilter {
				filteredHistory = append(filteredHistory, h)
			}
		}
	} else {
		filteredHistory = allHistory
	}

	data := map[string]interface{}{
		"Title":        "History",
		"History":      filteredHistory,
		"StatusFilter": statusFilter,
	}
	s.renderWithCSRF(w, r, "history.html", data)
}

// ==================== Pipeline Handlers ====================

// PipelineStats holds stats for the pipeline dashboard
type PipelineStats struct {
	EmailSent            int
	AwaitingResponse     int
	FormRequired         int
	FormFilled           int
	AwaitingCaptcha      int
	CaptchaSolved        int
	AwaitingConfirmation int
	Confirmed            int
	Rejected             int
	Failed               int
	PendingTasks         int
	NeedsReview          int
}

func (s *Server) getPipelineStats(profileID string) PipelineStats {
	stats := PipelineStats{}

	if s.historyStore == nil {
		return stats
	}

	// Get pipeline stage counts
	pipelineStats, err := s.historyStore.GetPipelineStats(profileID)
	if err == nil {
		stats.EmailSent = pipelineStats[history.PipelineEmailSent]
		stats.AwaitingResponse = pipelineStats[history.PipelineAwaitingResponse]
		stats.FormRequired = pipelineStats[history.PipelineFormRequired]
		stats.FormFilled = pipelineStats[history.PipelineFormFilled]
		stats.AwaitingCaptcha = pipelineStats[history.PipelineAwaitingCaptcha]
		stats.CaptchaSolved = pipelineStats[history.PipelineCaptchaSolved]
		stats.AwaitingConfirmation = pipelineStats[history.PipelineAwaitingConfirmation]
		stats.Confirmed = pipelineStats[history.PipelineConfirmed]
		stats.Rejected = pipelineStats[history.PipelineRejected]
		stats.Failed = pipelineStats[history.PipelineFailed]
	}

	// Get pending tasks count (CAPTCHAs, etc.)
	pendingTaskCount, _, _, err := s.historyStore.GetPendingTaskStats(profileID)
	if err == nil {
		stats.PendingTasks = pendingTaskCount
	}

	// Get needs review count
	responses, err := s.historyStore.GetBrokerResponses(profileID, "", true, 1000)
	if err == nil {
		stats.NeedsReview = len(responses)
	}

	// Get form stats (what's actually shown on tasks page)
	pendingForms, _, _, _, _, _ := s.historyStore.GetFormStats(profileID)

	// Calculate unified "Action Needed" based on what's displayed on /tasks page:
	// - Pending forms (forms without tasks yet)
	// - Pending tasks (from pending_tasks table)
	// - Items needing review (parser was unsure)
	stats.PendingTasks = pendingForms + pendingTaskCount + stats.NeedsReview

	return stats
}

func (s *Server) handlePipeline(w http.ResponseWriter, r *http.Request) {
	cfg := s.getConfig()
	if cfg == nil || cfg.Profile.FirstName == "" && len(cfg.Profiles) == 0 {
		http.Redirect(w, r, "/setup", http.StatusFound)
		return
	}

	active := s.activeProfile(r)
	pipelineStats := s.getPipelineStats(active.ID)

	// Get recent responses
	var recentResponses []history.BrokerResponse
	if s.historyStore != nil {
		recentResponses, _ = s.historyStore.GetBrokerResponses(active.ID, "", false, 20)
	}

	// Get pending tasks
	var pendingTasks []history.PendingTask
	if s.historyStore != nil {
		pendingTasks, _ = s.historyStore.GetPendingTasks(active.ID, "", "pending")
	}

	data := map[string]interface{}{
		"Title":           "Pipeline Status",
		"PipelineStats":   pipelineStats,
		"Responses":       recentResponses,
		"PendingTasks":    pendingTasks,
		"InboxConfigured": cfg.Inbox.Enabled,
	}

	s.renderWithCSRF(w, r, "pipeline.html", data)
}

func (s *Server) handleForms(w http.ResponseWriter, r *http.Request) {
	// Redirect to unified action needed page
	http.Redirect(w, r, "/tasks", http.StatusFound)
}

func (s *Server) handleFormComplete(w http.ResponseWriter, r *http.Request) {
	brokerID := chi.URLParam(r, "brokerID")

	if s.historyStore == nil {
		http.Error(w, "Database not available", http.StatusInternalServerError)
		return
	}

	// Update pipeline status to confirmed
	if err := s.historyStore.UpdatePipelineStatus(s.activeProfile(r).ID, brokerID, history.PipelineConfirmed); err != nil {
		http.Error(w, "Failed to update status", http.StatusInternalServerError)
		return
	}

	// If this was an HTMX request, return updated row HTML
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", "/tasks")
		w.WriteHeader(http.StatusOK)
		return
	}

	http.Redirect(w, r, "/tasks", http.StatusFound)
}

func (s *Server) handleFormSkip(w http.ResponseWriter, r *http.Request) {
	brokerID := chi.URLParam(r, "brokerID")

	if s.historyStore == nil {
		http.Error(w, "Database not available", http.StatusInternalServerError)
		return
	}

	// Update pipeline status to rejected (skipped)
	if err := s.historyStore.UpdatePipelineStatus(s.activeProfile(r).ID, brokerID, history.PipelineRejected); err != nil {
		http.Error(w, "Failed to update status", http.StatusInternalServerError)
		return
	}

	// If this was an HTMX request, return updated row HTML
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", "/tasks")
		w.WriteHeader(http.StatusOK)
		return
	}

	http.Redirect(w, r, "/tasks", http.StatusFound)
}

func (s *Server) handleTasks(w http.ResponseWriter, r *http.Request) {
	cfg := s.getConfig()
	if cfg == nil || cfg.Profile.FirstName == "" && len(cfg.Profiles) == 0 {
		http.Redirect(w, r, "/setup", http.StatusFound)
		return
	}

	active := s.activeProfile(r)
	taskType := r.URL.Query().Get("type")
	status := r.URL.Query().Get("status")
	if status == "" {
		status = "pending"
	}

	var tasks []history.PendingTask
	var completedTasksList []history.PendingTask
	var forms []history.FormWithStatus
	var reviewItems []history.BrokerResponse
	if s.historyStore != nil {
		tasks, _ = s.historyStore.GetPendingTasks(active.ID, history.TaskType(taskType), "pending")
		completedTasksList, _ = s.historyStore.GetPendingTasks(active.ID, history.TaskType(taskType), "completed")
		forms, _ = s.historyStore.GetFormsWithStatus(active.ID)
		// Get items needing review (parser was unsure)
		reviewItems, _ = s.historyStore.GetBrokerResponses(active.ID, "", true, 1000)
	}

	// Get task stats
	pendingTasks, completedTasksCount, skippedTasks := 0, 0, 0
	if s.historyStore != nil {
		pendingTasks, completedTasksCount, skippedTasks, _ = s.historyStore.GetPendingTaskStats(active.ID)
	}

	// Get form stats
	pendingForms, filledForms, captchaForms, failedForms, skippedForms := 0, 0, 0, 0, 0
	if s.historyStore != nil {
		pendingForms, filledForms, captchaForms, failedForms, skippedForms, _ = s.historyStore.GetFormStats(active.ID)
	}

	// Count forms needing action (only pending forms, not captcha since those are in pendingTasks)
	formsNeedingAction := pendingForms

	// Total action items: pending forms (without tasks) + pending tasks
	// This avoids double-counting captcha items
	totalActionItems := pendingForms + pendingTasks

	// Get items needing review count
	needsReviewCount := len(reviewItems)

	data := map[string]interface{}{
		"Title":              "Action Needed",
		"Tasks":              tasks,
		"CompletedTasksList": completedTasksList,
		"Forms":              forms,
		"ReviewItems":        reviewItems,
		"TaskType":           taskType,
		"Status":             status,
		"PendingTasks":       pendingTasks,
		"CompletedTasks":     completedTasksCount,
		"SkippedTasks":       skippedTasks,
		"PendingForms":       pendingForms,
		"FilledForms":        filledForms,
		"CaptchaForms":       captchaForms,
		"FailedForms":        failedForms,
		"SkippedForms":       skippedForms,
		"NeedsReview":        needsReviewCount,
		"FormsNeedingAction": formsNeedingAction,
		"TotalActionItems":   totalActionItems + needsReviewCount,
	}

	s.renderWithCSRF(w, r, "tasks.html", data)
}

func (s *Server) handleTaskDetail(w http.ResponseWriter, r *http.Request) {
	taskIDStr := chi.URLParam(r, "taskID")
	var taskID int64
	_, _ = fmt.Sscanf(taskIDStr, "%d", &taskID)

	if s.historyStore == nil {
		http.Error(w, "Database not available", http.StatusInternalServerError)
		return
	}

	task, err := s.historyStore.GetPendingTaskByID(taskID, s.activeProfile(r).ID)
	if err != nil || task == nil {
		http.Error(w, "Task not found", http.StatusNotFound)
		return
	}

	data := map[string]interface{}{
		"Title": "Task Detail",
		"Task":  task,
	}

	s.renderWithCSRF(w, r, "task-detail.html", data)
}

func (s *Server) handleTaskComplete(w http.ResponseWriter, r *http.Request) {
	taskIDStr := chi.URLParam(r, "taskID")
	var taskID int64
	_, _ = fmt.Sscanf(taskIDStr, "%d", &taskID)

	limitFormBody(w, r)
	status := r.FormValue("status")
	if status == "" {
		status = "completed"
	}

	if s.historyStore == nil {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`<span class="text-red-600">Database not available</span>`))
		return
	}

	if err := s.historyStore.CompletePendingTask(taskID, s.activeProfile(r).ID, status); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprintf(w, `<span class="text-red-600">Error: %s</span>`, template.HTMLEscapeString(err.Error()))
		return
	}

	// Redirect back to helper page to show updated status
	http.Redirect(w, r, fmt.Sprintf("/tasks/%d/helper", taskID), http.StatusFound)
}

func (s *Server) handleTaskSkip(w http.ResponseWriter, r *http.Request) {
	taskIDStr := chi.URLParam(r, "taskID")
	var taskID int64
	_, _ = fmt.Sscanf(taskIDStr, "%d", &taskID)

	if s.historyStore == nil {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`<span class="text-red-600">Database not available</span>`))
		return
	}

	if err := s.historyStore.CompletePendingTask(taskID, s.activeProfile(r).ID, "skipped"); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprintf(w, `<span class="text-red-600">Error: %s</span>`, template.HTMLEscapeString(err.Error()))
		return
	}

	// Redirect back to helper page to show updated status
	http.Redirect(w, r, fmt.Sprintf("/tasks/%d/helper", taskID), http.StatusFound)
}

func (s *Server) handleTaskHelper(w http.ResponseWriter, r *http.Request) {
	taskIDStr := chi.URLParam(r, "taskID")
	var taskID int64
	_, _ = fmt.Sscanf(taskIDStr, "%d", &taskID)

	if s.historyStore == nil {
		http.Error(w, "Database not available", http.StatusInternalServerError)
		return
	}

	activeProfileID := s.activeProfile(r).ID

	task, err := s.historyStore.GetPendingTaskByID(taskID, activeProfileID)
	if err != nil || task == nil {
		http.Error(w, "Task not found", http.StatusNotFound)
		return
	}

	// Mark task as opened (sets opened_at timestamp if not already set)
	_ = s.historyStore.MarkTaskOpened(taskID, activeProfileID)

	// Re-fetch task to get updated opened_at
	task, _ = s.historyStore.GetPendingTaskByID(taskID, activeProfileID)

	// Parse profile data from BrowserState (JSON)
	profileData := make(map[string]string)
	if task.BrowserState != "" {
		_ = json.Unmarshal([]byte(task.BrowserState), &profileData)
	}

	// Create ordered profile fields for display
	orderedFields := []struct {
		Key   string
		Label string
	}{
		{"email", "Email"},
		{"firstName", "First Name"},
		{"lastName", "Last Name"},
		{"phone", "Phone"},
		{"address", "Address"},
		{"city", "City"},
		{"state", "State"},
		{"zipCode", "ZIP Code"},
		{"country", "Country"},
	}

	// Build ordered map for template
	orderedProfile := make([]map[string]string, 0)
	for _, field := range orderedFields {
		if val, ok := profileData[field.Key]; ok && val != "" {
			orderedProfile = append(orderedProfile, map[string]string{
				"key":   field.Label,
				"value": val,
			})
		}
	}

	data := map[string]interface{}{
		"Title":          fmt.Sprintf("CAPTCHA Task: %s", task.BrokerName),
		"Task":           task,
		"ProfileData":    profileData,
		"OrderedProfile": orderedProfile,
	}

	s.renderWithCSRF(w, r, "task-helper.html", data)
}
