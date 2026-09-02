package web

import (
	"html/template"
	"net/http"
	"strings"
	"time"

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
	b := s.brokerDB.FindByID(brokerID)
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
	if cfg := s.getConfig(); cfg != nil && cfg.Email.From != "" {
		from = cfg.Email.From
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
	b := s.brokerDB.FindByID(brokerID)
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
