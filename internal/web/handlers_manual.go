package web

import (
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/drumandbytes/eraser/internal/broker"
	"github.com/drumandbytes/eraser/internal/history"
	"github.com/go-chi/chi/v5"
)

func (s *Server) configuredTemplate() string {
	if cfg := s.getConfig(); cfg != nil && cfg.Options.Template != "" {
		return cfg.Options.Template
	}
	return "gdpr"
}

// handleBrokerEmail renders the removal email for one broker so a manual-mode
// user can copy it, open it in their mail client, and mark it sent.
func (s *Server) handleBrokerEmail(w http.ResponseWriter, r *http.Request) {
	brokerID := chi.URLParam(r, "brokerID")
	b := s.brokers().FindByID(brokerID)
	if b == nil {
		http.NotFound(w, r)
		return
	}

	active := s.activeProfile(r)
	email, err := s.tmplEngine.Render(s.configuredTemplate(), active.Profile, *b)
	if err != nil {
		http.Error(w, "Failed to render email: "+err.Error(), http.StatusInternalServerError)
		return
	}

	from := active.Email
	if cfg := s.getConfig(); cfg != nil {
		if emailCfg := cfg.EmailForProfile(active); emailCfg.From != "" {
			from = emailCfg.From
		}
	}

	// mailto: query params - url.Values.Encode uses "+" for spaces, which some
	// mail clients drop into the body literally; %20 is safe everywhere.
	q := "subject=" + template.URLQueryEscaper(email.Subject) + "&body=" + template.URLQueryEscaper(email.Body)
	mailto := "mailto:" + b.Email + "?" + strings.ReplaceAll(q, "+", "%20")

	s.renderWithCSRF(w, r, "brokers/email.html", map[string]interface{}{
		"Title":     b.Name + " - removal email",
		"Broker":    b,
		"From":      from,
		"Subject":   email.Subject,
		"Body":      email.Body,
		"MailtoURL": template.URL(mailto), //nolint:gosec // mailto: scheme, recipient is from our own broker DB
	})
}

// handleAPIMarkSent records a manual send for one broker and returns the
// refreshed status badge (mirrors the exclude/include HTMX handlers).
func (s *Server) handleAPIMarkSent(w http.ResponseWriter, r *http.Request) {
	brokerID := chi.URLParam(r, "brokerID")
	b := s.brokers().FindByID(brokerID)
	if b == nil {
		http.Error(w, "Broker not found", http.StatusNotFound)
		return
	}
	if s.historyStore == nil {
		http.Error(w, "History database not available", http.StatusInternalServerError)
		return
	}

	rec := &history.Record{
		ProfileID:  s.activeProfile(r).ID,
		BrokerID:   b.ID,
		BrokerName: b.Name,
		Email:      b.Email,
		Template:   s.configuredTemplate(),
		Status:     history.StatusSent,
		SentAt:     time.Now(),
		SentMethod: "manual",
	}
	if err := s.historyStore.Add(rec); err != nil {
		http.Error(w, "Failed to record: "+err.Error(), http.StatusInternalServerError)
		return
	}

	s.renderPartial(w, "partials/broker-status-badge.html", map[string]interface{}{
		"ID":     b.ID,
		"Status": "sent",
	})
}

// handleAPIMarkBounced is `eraser mark-bounced` for one broker: flips the
// active profile's latest "sent" record to failed so the next send retries it.
func (s *Server) handleAPIMarkBounced(w http.ResponseWriter, r *http.Request) {
	brokerID := chi.URLParam(r, "brokerID")
	b := s.brokers().FindByID(brokerID)
	if b == nil {
		http.Error(w, "Broker not found", http.StatusNotFound)
		return
	}
	if s.historyStore == nil {
		http.Error(w, "History database not available", http.StatusInternalServerError)
		return
	}
	if _, err := s.historyStore.MarkFailed(s.activeProfile(r).ID, b.ID, "bounced - manually confirmed"); err != nil {
		http.Error(w, "Failed to record: "+err.Error(), http.StatusInternalServerError)
		return
	}
	// re-render from history: "failed", or unchanged if there was no sent record
	s.handleAPIBrokerStatus(w, r)
}

// handleBrokerNew is `eraser add-broker`: the entry goes to the user's own
// brokers file (broker.LocalPath), which update-brokers never touches.
func (s *Server) handleBrokerNew(w http.ResponseWriter, r *http.Request) {
	render := func(b broker.Broker, errors map[string]string) {
		s.renderWithCSRF(w, r, "brokers/new.html", map[string]interface{}{
			"Title":      "Add Broker",
			"Broker":     b,
			"Errors":     errors,
			"Categories": s.getUniqueCategories(),
		})
	}
	if r.Method != http.MethodPost {
		render(broker.Broker{Region: "eu"}, map[string]string{})
		return
	}
	limitFormBody(w, r)
	b := broker.Broker{
		Name:      strings.TrimSpace(r.FormValue("name")),
		Email:     strings.TrimSpace(r.FormValue("email")),
		OptOutURL: strings.TrimSpace(r.FormValue("opt_out_url")),
		Website:   strings.TrimSpace(r.FormValue("website")),
		Region:    strings.TrimSpace(r.FormValue("region")),
		Category:  strings.ToLower(strings.TrimSpace(r.FormValue("category"))),
	}
	b.ID = broker.NewID(b.Name)

	errors := map[string]string{}
	if b.ID == "" {
		errors["name"] = "Company name is required"
	} else if s.brokers().FindByID(b.ID) != nil || s.brokers().FindByName(b.Name) != nil {
		errors["name"] = "A broker with this name is already in the list"
	}
	if b.Email == "" && b.OptOutURL == "" {
		errors["email"] = "Give a privacy email or an opt-out form URL"
	}
	for _, p := range b.Problems() {
		switch {
		case strings.Contains(p, "email"):
			errors["email"] = "That doesn't look like an email address"
		case strings.Contains(p, "opt_out_url"):
			errors["opt_out_url"] = "Use a full http(s):// address"
		case strings.Contains(p, "website"):
			errors["website"] = "Use a full http(s):// address"
		case strings.Contains(p, "region"):
			errors["_"] = "Pick a region"
		}
	}
	if len(errors) > 0 {
		render(b, errors)
		return
	}

	if err := broker.SaveLocal(b); err != nil {
		render(b, map[string]string{"_": "Failed to save: " + err.Error()})
		return
	}
	if err := s.reloadBrokers(); err != nil {
		render(b, map[string]string{"_": "Saved, but reloading the list failed: " + err.Error()})
		return
	}
	http.Redirect(w, r, "/brokers?search="+url.QueryEscape(b.ID), http.StatusSeeOther)
}
