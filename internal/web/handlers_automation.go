package web

import (
	"encoding/json"
	"net/http"
)

// approvalPreview is the part of a batch preview the approver sees.
type approvalPreview struct {
	PlanID        string            `json:"-"`
	ProfileID     string            `json:"profile_id"`
	ProfileFields map[string]string `json:"profile_fields"`
	Tasks         []struct {
		BrokerName string `json:"broker_name"`
		Kind       string `json:"kind"`
		URL        string `json:"url"`
	} `json:"tasks"`
	Emails []struct {
		BrokerID string `json:"broker_id"`
		To       string `json:"to"`
		From     string `json:"from"`
		Subject  string `json:"subject"`
		Body     string `json:"body"`
	} `json:"emails"`
}

func (s *Server) handleAutomation(w http.ResponseWriter, r *http.Request) {
	if s.historyStore == nil {
		http.Error(w, "Database not available", http.StatusInternalServerError)
		return
	}
	pending, err := s.historyStore.PendingBatchApprovals()
	if err != nil {
		http.Error(w, "Unable to load approvals", http.StatusInternalServerError)
		return
	}
	var previews []approvalPreview
	for _, a := range pending {
		var p approvalPreview
		if err := json.Unmarshal([]byte(a.Preview), &p); err != nil {
			continue
		}
		p.PlanID = a.PlanID
		previews = append(previews, p)
	}
	s.renderWithCSRF(w, r, "automation.html", map[string]interface{}{
		"Title": "Automation approvals", "Previews": previews,
	})
}

// handleAutomationApprove approves one batch. No token: browsers can't post this
// cross-site (filippo.io/csrf checks Sec-Fetch-Site).
func (s *Server) handleAutomationApprove(w http.ResponseWriter, r *http.Request) {
	if s.historyStore == nil {
		http.Error(w, "Database not available", http.StatusInternalServerError)
		return
	}
	planID := r.FormValue("plan_id")
	if err := s.historyStore.ApproveBatch(planID, "web"); err != nil {
		http.Error(w, "This batch is no longer waiting for approval. Prepare a new one.", http.StatusConflict)
		return
	}
	http.Redirect(w, r, "/automation", http.StatusSeeOther)
}
