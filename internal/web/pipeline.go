package web

import (
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/drumandbytes/eraser/internal/history"
	"golang.org/x/net/html"
)

const pipelinePageSize = 50

type pipelineFilter struct {
	View, Status, Type, Search string
	NeedsReview                bool
	Page                       int
}

func readPipelineFilter(r *http.Request) pipelineFilter {
	q := r.URL.Query()
	f := pipelineFilter{View: q.Get("view"), Status: q.Get("status"), Type: q.Get("type"), Search: strings.TrimSpace(q.Get("q")), NeedsReview: q.Get("needs_review") == "true"}
	if f.View == "" && f.Status != "" {
		f.View = "requests"
	}
	if f.View != "responses" && f.View != "requests" {
		f.View = "actions"
	}
	if f.View != "responses" {
		f.Type = ""
	}
	if f.View != "requests" {
		f.Status = ""
	}
	if f.View == "requests" {
		f.NeedsReview = false
	}
	f.Page, _ = strconv.Atoi(q.Get("page"))
	if f.Page < 1 || f.Page > 1000000 {
		f.Page = 1
	}
	return f
}

func (f pipelineFilter) URL() string {
	q := url.Values{"view": {f.View}}
	if f.Status != "" {
		q.Set("status", f.Status)
	}
	if f.Type != "" {
		q.Set("type", f.Type)
	}
	if f.Search != "" {
		q.Set("q", f.Search)
	}
	if f.NeedsReview {
		q.Set("needs_review", "true")
	}
	if f.Page > 1 {
		q.Set("page", strconv.Itoa(f.Page))
	}
	return "/pipeline?" + q.Encode()
}

type pipelineItem struct {
	BrokerName, BrokerID, Kind, Label, Subject, Snippet, URL, Action, FormURL string
	At                                                                        time.Time
	ID                                                                        int64
	NeedsReview                                                               bool
}
type pipelineCard struct {
	Label, URL string
	Count      int
	Active     bool
}
type pipelineView struct {
	Filter                                                                pipelineFilter
	Cards                                                                 []pipelineCard
	Items                                                                 []pipelineItem
	Total, Start, End, Pages                                              int
	PreviousURL, NextURL, ActionsURL, ResponsesURL, RequestsURL, ClearURL string
	Heading, LoadError                                                    string
}

func pipelineLabel(kind string) string {
	labels := map[string]string{
		"email_sent": "Email sent", "awaiting_response": "Awaiting response", "form_required": "Form required",
		"form_filled": "Form filled", "awaiting_captcha": "Awaiting CAPTCHA", "captcha_solved": "CAPTCHA solved",
		"awaiting_confirmation": "Awaiting confirmation", "confirmed": "Confirmed", "success": "Deletion confirmed",
		"no_data_held": "No data held", "rejected": "Rejected", "failed": "Failed", "pending": "Processing",
		"unknown": "Unknown", "bounced": "Bounced", "manual_form": "Form required", "form": "Form required",
		"confirmation_required": "Confirmation required", "confirm": "Confirmation required", "confirmation": "Confirmation required",
		"captcha": "CAPTCHA", "review": "Needs review",
	}
	if label, ok := labels[kind]; ok {
		return label
	}
	return strings.ReplaceAll(kind, "_", " ")
}

// responsePreview is plain text, still escaped by html/template. Matching
// snippets let a body-only search explain why a reply was returned.
func responsePreview(body, search string) string {
	z := html.NewTokenizer(strings.NewReader(body))
	var b strings.Builder
	skip := 0
	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			break
		}
		if tt == html.StartTagToken || tt == html.EndTagToken {
			name, _ := z.TagName()
			if string(name) == "script" || string(name) == "style" {
				if tt == html.StartTagToken {
					skip++
				} else if skip > 0 {
					skip--
				}
			}
			b.WriteByte(' ')
		}
		if tt == html.TextToken && skip == 0 {
			b.Write(z.Text())
		}
	}
	text := strings.Join(strings.Fields(b.String()), " ")
	runes := []rune(text)
	start := 0
	if search != "" {
		// One rune in, one rune out, so match indexes line up with runes.
		lower := make([]rune, len(runes))
		for i, r := range runes {
			lower[i] = unicode.ToLower(r)
		}
		if at := strings.Index(string(lower), strings.ToLower(search)); at > 0 {
			start = max(0, utf8.RuneCountInString(string(lower)[:at])-55)
		}
	}
	end := min(len(runes), start+220)
	preview := string(runes[start:end])
	if start > 0 {
		preview = "…" + preview
	}
	if end < len(runes) {
		preview += "…"
	}
	return preview
}

func (s *Server) loadPipelineStats(profileID string) (PipelineStats, history.ActionItems, error) {
	var stats PipelineStats
	var actions history.ActionItems
	if s.historyStore == nil {
		return stats, actions, fmt.Errorf("database not available")
	}
	stages, err := s.historyStore.GetPipelineStats(profileID)
	if err != nil {
		return stats, actions, err
	}
	actions, err = s.historyStore.GetActionItems(profileID)
	if err != nil {
		return stats, actions, err
	}
	stats = PipelineStats{
		EmailSent: stages[history.PipelineEmailSent], AwaitingResponse: stages[history.PipelineAwaitingResponse],
		FormRequired: stages[history.PipelineFormRequired], FormFilled: stages[history.PipelineFormFilled],
		AwaitingCaptcha: stages[history.PipelineAwaitingCaptcha], CaptchaSolved: stages[history.PipelineCaptchaSolved],
		AwaitingConfirmation: stages[history.PipelineAwaitingConfirmation], Confirmed: stages[history.PipelineConfirmed],
		NoData: stages[history.PipelineNoData], Rejected: stages[history.PipelineRejected], Failed: stages[history.PipelineFailed],
		PendingTasks: actions.Total(), NeedsReview: actions.NeedsReview(),
	}
	return stats, actions, nil
}

func newPipelineView(f pipelineFilter, stats PipelineStats) pipelineView {
	v := pipelineView{Filter: f, Heading: "Action Required"}
	for _, card := range []struct {
		label, status string
		count         int
	}{
		{"Email Sent", "email_sent", stats.EmailSent}, {"Awaiting Response", "awaiting_response", stats.AwaitingResponse},
		{"Form Required", "form_required", stats.FormRequired}, {"Awaiting Confirm", "awaiting_confirmation", stats.AwaitingConfirmation},
		{"Confirmed", "confirmed", stats.Confirmed}, {"No data held", "no_data_held", stats.NoData}, {"Rejected", "rejected", stats.Rejected},
		{"Pending Tasks", "actions", stats.PendingTasks}, {"Needs Review", "review", stats.NeedsReview},
	} {
		cf := pipelineFilter{View: "requests", Status: card.status, Search: f.Search, Page: 1}
		active := f.View == "requests" && f.Status == card.status
		if card.status == "actions" || card.status == "review" {
			cf.View, cf.Status, cf.NeedsReview = "actions", "", card.status == "review"
			active = f.View == "actions" && f.NeedsReview == cf.NeedsReview
		}
		v.Cards = append(v.Cards, pipelineCard{card.label, cf.URL(), card.count, active})
	}
	for _, name := range []string{"actions", "responses", "requests"} {
		u := (pipelineFilter{View: name, Search: f.Search, Page: 1}).URL()
		switch name {
		case "actions":
			v.ActionsURL = u
		case "responses":
			v.ResponsesURL = u
		case "requests":
			v.RequestsURL = u
		}
	}
	clear := f
	clear.Search = ""
	clear.Type = ""
	clear.Status = ""
	clear.NeedsReview = false
	clear.Page = 1
	v.ClearURL = clear.URL()
	if f.View == "responses" {
		v.Heading = "Responses"
	}
	if f.View == "requests" {
		v.Heading = "Requests"
		if f.Status != "" {
			v.Heading = pipelineLabel(f.Status)
		}
	}
	if f.NeedsReview {
		v.Heading = "Needs Review"
	}
	return v
}

func (s *Server) pipelineResults(profileID string, f pipelineFilter, stats PipelineStats, actions history.ActionItems) (pipelineView, error) {
	v := newPipelineView(f, stats)
	if s.historyStore == nil {
		return v, fmt.Errorf("database not available")
	}
	if f.View == "responses" {
		responses, total, err := s.historyStore.QueryBrokerResponses(profileID, history.ResponseQuery{Type: f.Type, NeedsReview: f.NeedsReview, Search: f.Search, Limit: pipelinePageSize, Offset: (f.Page - 1) * pipelinePageSize})
		if err != nil {
			return v, err
		}
		v.Total = total
		if f.Page > 1 && (f.Page-1)*pipelinePageSize >= total {
			f.Page = 1
			return s.pipelineResults(profileID, f, stats, actions)
		}
		for _, r := range responses {
			v.Items = append(v.Items, pipelineItem{BrokerName: r.BrokerName, BrokerID: r.BrokerID, Kind: r.ResponseType, Label: pipelineLabel(r.ResponseType), Subject: r.EmailSubject, Snippet: responsePreview(r.EmailBody, f.Search), URL: fmt.Sprintf("/pipeline/responses/%d", r.ID), Action: "Read response", At: r.ReceivedAt, ID: r.ID, NeedsReview: r.NeedsReview})
		}
	} else {
		matches := func(parts ...string) bool {
			return f.Search == "" || strings.Contains(strings.ToLower(strings.Join(parts, " ")), strings.ToLower(f.Search))
		}
		if f.View == "requests" {
			requests, err := s.historyStore.LatestRequests(profileID)
			if err != nil {
				return v, err
			}
			for _, r := range requests {
				if (f.Status != "" && string(r.PipelineStatus) != f.Status) || !matches(r.BrokerName, r.BrokerID, r.Email) {
					continue
				}
				v.Items = append(v.Items, pipelineItem{BrokerName: r.BrokerName, BrokerID: r.BrokerID, Kind: string(r.PipelineStatus), Label: pipelineLabel(string(r.PipelineStatus)), Subject: r.Email, URL: "/brokers?search=" + url.QueryEscape(r.BrokerName), Action: "View broker", At: r.SentAt, ID: r.ID})
			}
		} else {
			var matching map[int64]bool
			if f.Search != "" {
				var err error
				matching, err = s.historyStore.MatchingResponseIDs(profileID, f.Search)
				if err != nil {
					return v, err
				}
			}
			for _, form := range actions.Forms {
				if f.NeedsReview || (!matches(form.BrokerName, form.BrokerID, form.EmailSubject) && !matching[form.ResponseID]) {
					continue
				}
				v.Items = append(v.Items, pipelineItem{BrokerName: form.BrokerName, BrokerID: form.BrokerID, Kind: "form", Label: "Form required", Subject: form.EmailSubject, URL: "/tasks#form-" + form.BrokerID, Action: "View form", FormURL: form.FormURL, At: form.DetectedAt, ID: form.ResponseID})
			}
			for _, task := range actions.Tasks {
				review := task.TaskType == history.TaskReview
				if (f.NeedsReview && !review) || !matches(task.BrokerName, task.BrokerID, task.Notes, string(task.TaskType)) {
					continue
				}
				v.Items = append(v.Items, pipelineItem{BrokerName: task.BrokerName, BrokerID: task.BrokerID, Kind: string(task.TaskType), Label: pipelineLabel(string(task.TaskType)), Snippet: responsePreview(task.Notes, f.Search), URL: fmt.Sprintf("/tasks/%d", task.ID), Action: "View task", At: task.CreatedAt, ID: task.ID, NeedsReview: review})
			}
			for _, r := range actions.Reviews {
				if !matches(r.BrokerName, r.BrokerID, r.EmailSubject, r.EmailFrom) && !matching[r.ID] {
					continue
				}
				v.Items = append(v.Items, pipelineItem{BrokerName: r.BrokerName, BrokerID: r.BrokerID, Kind: r.ResponseType, Label: pipelineLabel(r.ResponseType), Subject: r.EmailSubject, URL: fmt.Sprintf("/pipeline/responses/%d", r.ID), Action: "Review response", At: r.ReceivedAt, ID: r.ID, NeedsReview: true})
			}
		}
		sort.SliceStable(v.Items, func(i, j int) bool {
			a, b := v.Items[i], v.Items[j]
			if a.At.Equal(b.At) {
				return a.ID > b.ID
			}
			return a.At.After(b.At)
		})
		v.Total = len(v.Items)
		if f.Page > 1 && (f.Page-1)*pipelinePageSize >= v.Total {
			f.Page = 1
		}
		start := (f.Page - 1) * pipelinePageSize
		v.Items = v.Items[start:min(start+pipelinePageSize, v.Total)]
	}
	v.Filter = f
	v.Pages = max(1, (v.Total+pipelinePageSize-1)/pipelinePageSize)
	if v.Total > 0 {
		v.Start = (f.Page-1)*pipelinePageSize + 1
		v.End = v.Start + len(v.Items) - 1
	}
	if f.Page > 1 {
		prev := f
		prev.Page--
		v.PreviousURL = prev.URL()
	}
	if f.Page < v.Pages {
		next := f
		next.Page++
		v.NextURL = next.URL()
	}
	return v, nil
}

func (s *Server) handlePipeline(w http.ResponseWriter, r *http.Request) {
	cfg := s.getConfig()
	if cfg == nil || !cfg.HasProfile() {
		http.Redirect(w, r, "/setup", http.StatusFound)
		return
	}
	active := s.activeProfile(r)
	f := readPipelineFilter(r)
	stats, actions, err := s.loadPipelineStats(active.ID)
	v := newPipelineView(f, stats)
	if err == nil {
		v, err = s.pipelineResults(active.ID, f, stats, actions)
	}
	if err != nil {
		v.LoadError = "Unable to load pipeline items. Please try again."
	}
	w.Header().Add("Vary", "HX-Request")
	w.Header().Add("Vary", "HX-History-Restore-Request")
	// History restoration needs the full document, not only the list fragment.
	if r.Header.Get("HX-Request") == "true" && r.Header.Get("HX-History-Restore-Request") != "true" {
		w.Header().Set("HX-Push-Url", v.Filter.URL())
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
		}
		s.renderPartial(w, "partials/pipeline-content.html", v)
		return
	}
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
	}
	s.renderWithCSRF(w, r, "pipeline.html", map[string]interface{}{"Title": "Pipeline", "Pipeline": v, "InboxConfigured": cfg.InboxForProfile(active).Enabled})
}
