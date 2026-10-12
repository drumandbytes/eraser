package history

import (
	"fmt"
	"sort"
	"strings"
)

// ActionItems is the common source for action rows and counters in both web
// pages. Forms represented by tasks appear only once. Reviews are independent
// actions and are not restricted to the response type "unknown".
type ActionItems struct {
	Forms   []FormWithStatus
	Tasks   []PendingTask
	Reviews []BrokerResponse
}

func (a ActionItems) Total() int { return len(a.Forms) + len(a.Tasks) + len(a.Reviews) }
func (a ActionItems) NeedsReview() int {
	n := len(a.Reviews)
	for _, t := range a.Tasks {
		if t.TaskType == TaskReview {
			n++
		}
	}
	return n
}
func (a ActionItems) FormsToFill() int {
	n := len(a.Forms)
	for _, t := range a.Tasks {
		if t.TaskType == TaskManualForm {
			n++
		}
	}
	return n
}

func (s *Store) GetActionItems(profileID string) (ActionItems, error) {
	var a ActionItems
	forms, err := s.GetFormsWithStatus(profileID)
	if err != nil {
		return a, err
	}
	a.Tasks, err = s.GetPendingTasks(profileID, "", "pending")
	if err != nil {
		return a, err
	}
	withTask := make(map[string]bool)
	for _, t := range a.Tasks {
		if t.TaskType == TaskManualForm || t.TaskType == TaskCaptcha {
			withTask[t.BrokerID] = true
		}
	}
	for _, f := range forms {
		if f.Status == "pending" && !withTask[f.BrokerID] {
			a.Forms = append(a.Forms, f)
		}
	}
	a.Reviews, err = s.GetBrokerResponses(profileID, "", true, -1)
	return a, err
}

// ResponseQuery searches the entire active profile before applying pagination.
type ResponseQuery struct {
	Type        string
	NeedsReview bool
	Search      string
	Limit       int
	Offset      int
}

func responseWhere(profileID string, q ResponseQuery) (string, []any) {
	where := "profile_id = ?"
	args := []any{normalizeProfileID(profileID)}
	if q.Type != "" {
		where += " AND response_type = ?"
		args = append(args, q.Type)
	}
	if q.NeedsReview {
		where += " AND needs_review = 1"
	}
	if search := strings.TrimSpace(q.Search); search != "" {
		// Match Unicode case-insensitively, with %, _ and backslashes treated literally.
		where += ` AND (eraser_contains(broker_name, ?) OR eraser_contains(broker_id, ?) OR
			eraser_contains(email_from, ?) OR eraser_contains(email_subject, ?) OR eraser_contains(email_body, ?))`
		for range 5 {
			args = append(args, search)
		}
	}
	return where, args
}

func (s *Store) QueryBrokerResponses(profileID string, q ResponseQuery) ([]BrokerResponse, int, error) {
	where, args := responseWhere(profileID, q)
	var total int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM broker_responses WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.queryBrokerResponses(`SELECT `+brokerResponseCols+` FROM broker_responses WHERE `+where+` ORDER BY received_at DESC, id DESC LIMIT ? OFFSET ?`, append(args, q.Limit, q.Offset)...)
	return rows, total, err
}

// MatchingResponseIDs lets action searches include a form/review's underlying
// email text without loading every body into the web response.
func (s *Store) MatchingResponseIDs(profileID, search string) (map[int64]bool, error) {
	where, args := responseWhere(profileID, ResponseQuery{Search: search})
	rows, err := s.db.Query(`SELECT id FROM broker_responses WHERE `+where, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	ids := make(map[int64]bool)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids[id] = true
	}
	return ids, rows.Err()
}

// LatestRequests uses the same most-recent-send ordering as status updates.
// Historical sends do not inflate stage counters or their drill-down lists.
func (s *Store) LatestRequests(profileID string) ([]Record, error) {
	all, err := s.GetAllRequests(profileID)
	if err != nil {
		return nil, fmt.Errorf("latest pipeline requests: %w", err)
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].SentAt.Equal(all[j].SentAt) {
			return all[i].ID < all[j].ID
		}
		return all[i].SentAt.Before(all[j].SentAt)
	})
	seen := make(map[string]bool)
	var latest []Record
	for i := len(all) - 1; i >= 0; i-- {
		r := all[i]
		if seen[r.BrokerID] {
			continue
		}
		seen[r.BrokerID] = true
		if r.PipelineStatus == "" {
			r.PipelineStatus = PipelineEmailSent
		}
		latest = append(latest, r)
	}
	return latest, nil
}
