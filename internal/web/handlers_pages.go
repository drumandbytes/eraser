package web

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"time"

	"github.com/drumandbytes/eraser/internal/evidence"
	"github.com/drumandbytes/eraser/internal/history"
	"github.com/go-chi/chi/v5"
)

// Handler implementations

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	cfg := s.getConfig()
	if cfg == nil || !cfg.HasProfile() {
		http.Redirect(w, r, "/setup", http.StatusFound)
		return
	}

	active := s.activeProfile(r)
	data := map[string]interface{}{
		"Title":         "Dashboard",
		"Profile":       active.Profile,
		"BrokerCount":   len(s.brokers().Brokers),
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

	brokers := s.getBrokersWithStatus(s.activeProfile(r).ID, search, category, region, status, nil, nil, missingEmail, showExcluded)

	dailyLimit := effectiveDailyLimit(s.getConfig())
	hidden, country := s.countryHidden(s.activeProfile(r).ID)

	data := map[string]interface{}{
		"Title":         "Data Brokers",
		"Brokers":       brokers,
		"Categories":    s.getUniqueCategories(),
		"Regions":       s.getUniqueRegions(),
		"Search":        search,
		"Category":      category,
		"Region":        region,
		"Status":        status,
		"MissingEmail":  missingEmail,
		"ShowExcluded":  showExcluded,
		"Total":         len(s.brokers().Brokers),
		"Filtered":      len(brokers),
		"CountryHidden": hidden,
		"Country":       country,
		"DailyLimit":    dailyLimit,
		"ManualMode":    s.getConfig() != nil && s.getConfig().IsManualSend(),
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
	NoData               int
	Rejected             int
	Failed               int
	PendingTasks         int
	NeedsReview          int
}

func (s *Server) getPipelineStats(profileID string) PipelineStats {
	stats, _, _ := s.loadPipelineStats(profileID)
	return stats
}

func (s *Server) handleResponseReview(w http.ResponseWriter, r *http.Request) {
	var responseID int64
	if _, err := fmt.Sscanf(chi.URLParam(r, "responseID"), "%d", &responseID); err != nil {
		http.Error(w, "Response not found", http.StatusNotFound)
		return
	}
	if s.historyStore == nil {
		http.Error(w, "Database not available", http.StatusInternalServerError)
		return
	}
	response, err := s.historyStore.GetBrokerResponseByID(responseID, s.activeProfile(r).ID)
	if err != nil || response == nil {
		http.Error(w, "Response not found", http.StatusNotFound)
		return
	}
	s.renderWithCSRF(w, r, "response-review.html", map[string]interface{}{
		"Title":    "Review Response",
		"Response": response,
	})
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

	if err := s.historyStore.OverridePipelineStatus(s.activeProfile(r).ID, brokerID, history.PipelineConfirmed); err != nil {
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

	if err := s.historyStore.OverridePipelineStatus(s.activeProfile(r).ID, brokerID, history.PipelineRejected); err != nil {
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
	if cfg == nil || !cfg.HasProfile() {
		http.Redirect(w, r, "/setup", http.StatusFound)
		return
	}

	active := s.activeProfile(r)
	taskType := r.URL.Query().Get("type")
	status := r.URL.Query().Get("status")
	if status == "" {
		status = "pending"
	}

	var actions history.ActionItems
	var forms []history.FormWithStatus
	var completedTasksList []history.PendingTask
	var completedTasksCount, skippedTasks, filledForms, captchaForms, failedForms, skippedForms int
	if s.historyStore != nil {
		var err error
		actions, err = s.historyStore.GetActionItems(active.ID)
		if err != nil {
			http.Error(w, "Unable to load action items", http.StatusInternalServerError)
			return
		}
		forms, err = s.historyStore.GetFormsWithStatus(active.ID)
		if err != nil {
			http.Error(w, "Unable to load forms", http.StatusInternalServerError)
			return
		}
		// Pending forms come exclusively from the shared action projection.
		kept := append([]history.FormWithStatus{}, actions.Forms...)
		for _, f := range forms {
			if f.Status != "pending" {
				kept = append(kept, f)
			}
		}
		forms = kept
		completedTasksList, _ = s.historyStore.GetPendingTasks(active.ID, "", "completed")
		_, completedTasksCount, skippedTasks, _ = s.historyStore.GetPendingTaskStats(active.ID)
		_, filledForms, captchaForms, failedForms, skippedForms, _ = s.historyStore.GetFormStats(active.ID)
	}

	data := map[string]interface{}{
		"Title":              "Action Needed",
		"Tasks":              actions.Tasks,
		"CompletedTasksList": completedTasksList,
		"Forms":              forms,
		"ReviewItems":        actions.Reviews,
		"TaskType":           taskType,
		"Status":             status,
		"PendingTasks":       len(actions.Tasks),
		"CompletedTasks":     completedTasksCount,
		"SkippedTasks":       skippedTasks,
		"PendingForms":       len(actions.Forms),
		"FilledForms":        filledForms,
		"CaptchaForms":       captchaForms,
		"FailedForms":        failedForms,
		"SkippedForms":       skippedForms,
		"NeedsReview":        actions.NeedsReview(),
		"FormsNeedingAction": actions.FormsToFill(),
		"TotalActionItems":   actions.Total(),
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
		_, _ = w.Write([]byte(`<span class="text-error">Database not available</span>`))
		return
	}

	if err := s.historyStore.CompletePendingTask(taskID, s.activeProfile(r).ID, status); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprintf(w, `<span class="text-error">Error: %s</span>`, template.HTMLEscapeString(err.Error()))
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
		_, _ = w.Write([]byte(`<span class="text-error">Database not available</span>`))
		return
	}

	if err := s.historyStore.CompletePendingTask(taskID, s.activeProfile(r).ID, "skipped"); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprintf(w, `<span class="text-error">Error: %s</span>`, template.HTMLEscapeString(err.Error()))
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

	profileData := make(map[string]string)
	if task.BrowserState != "" {
		_ = json.Unmarshal([]byte(task.BrowserState), &profileData)
	}

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

// handleExport downloads the active profile's evidence report, the same one
// `eraser export` writes (?format=html|json, optional ?since=YYYY-MM-DD).
func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	format := r.URL.Query().Get("format")
	if format == "" {
		format = "html"
	}
	if format != "html" && format != "json" {
		http.Error(w, "format must be html or json", http.StatusBadRequest)
		return
	}
	var since time.Time
	if v := r.URL.Query().Get("since"); v != "" {
		t, err := time.Parse("2006-01-02", v)
		if err != nil {
			http.Error(w, "since must be YYYY-MM-DD", http.StatusBadRequest)
			return
		}
		since = t
	}
	if s.historyStore == nil {
		http.Error(w, "History database not available", http.StatusInternalServerError)
		return
	}

	profile := s.activeProfile(r)
	requests, err := s.historyStore.GetAllRequests(profile.ID)
	if err != nil {
		http.Error(w, "Failed to read requests: "+err.Error(), http.StatusInternalServerError)
		return
	}
	responses, err := s.historyStore.GetBrokerResponsesForExport(profile.ID)
	if err != nil {
		http.Error(w, "Failed to read responses: "+err.Error(), http.StatusInternalServerError)
		return
	}
	now := time.Now()
	report := evidence.Build(profile.Profile, requests, responses, s.brokers(), s.tmplEngine, since, now)

	var data []byte
	contentType := "text/html; charset=utf-8"
	if format == "json" {
		data, err = json.MarshalIndent(report, "", "  ")
		contentType = "application/json"
	} else {
		data, err = evidence.RenderHTML(report)
	}
	if err != nil {
		http.Error(w, "Failed to render report: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="eraser-evidence-%s-%s.%s"`, profile.ID, now.Format("2006-01-02"), format))
	_, _ = w.Write(data)
}
