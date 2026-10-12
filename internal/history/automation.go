package history

import (
	"database/sql"
	"fmt"
	"time"
)

// AutomationAttempt records actions before they reach a broker. An interrupted
// attempt must be reviewed rather than blindly replayed after a restart.
type AutomationAttempt struct {
	Key     string `json:"key"`
	Subject string `json:"subject"`
	State   string `json:"state"`
	Note    string `json:"note"`
}

func (s *Store) migrateAutomation() error {
	_, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS automation_attempts (
		profile_id TEXT NOT NULL, action_key TEXT NOT NULL, subject TEXT NOT NULL,
		state TEXT NOT NULL, note TEXT NOT NULL DEFAULT '', updated_at DATETIME NOT NULL,
		PRIMARY KEY (profile_id, action_key)
	);
	CREATE INDEX IF NOT EXISTS idx_automation_subject ON automation_attempts(profile_id, subject, state);`)
	return err
}

// ClaimAutomationAction is atomic across MCP processes. A crash leaves an
// unknown receipt, preventing an automatic retry of a possible submission.
func (s *Store) ClaimAutomationAction(profileID, key, subject string) error {
	r, err := s.db.Exec(`INSERT INTO automation_attempts
		(profile_id, action_key, subject, state, updated_at)
		SELECT ?, ?, ?, 'unknown', ? WHERE NOT EXISTS
		(SELECT 1 FROM automation_attempts WHERE profile_id = ? AND subject = ?
		AND (state = 'unknown' OR (subject LIKE 'task:%' AND state = 'submitted')))
		ON CONFLICT(profile_id, action_key) DO NOTHING`, normalizeProfileID(profileID), key, subject,
		time.Now(), normalizeProfileID(profileID), subject)
	if err != nil {
		return fmt.Errorf("record automation attempt: %w", err)
	}
	n, err := r.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("action already attempted or an earlier outcome is unknown; inspect the task before retrying")
	}
	return nil
}

func (s *Store) FinishAutomationAction(profileID, key, state, note string) error {
	switch state {
	case "progress", "submitted", "unknown":
	default:
		return fmt.Errorf("invalid automation outcome")
	}
	r, err := s.db.Exec(`UPDATE automation_attempts SET state = ?, note = ?, updated_at = ?
		WHERE profile_id = ? AND action_key = ?`, state, note, time.Now(), normalizeProfileID(profileID), key)
	if err != nil {
		return err
	}
	n, err := r.RowsAffected()
	if err == nil && n == 0 {
		return sql.ErrNoRows
	}
	return err
}

func (s *Store) GetAutomationAttempts(profileID, subject string) ([]AutomationAttempt, error) {
	rows, err := s.db.Query(`SELECT action_key, subject, state, note FROM automation_attempts
		WHERE profile_id = ? AND subject = ? ORDER BY updated_at`, normalizeProfileID(profileID), subject)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []AutomationAttempt{}
	for rows.Next() {
		var a AutomationAttempt
		if err := rows.Scan(&a.Key, &a.Subject, &a.State, &a.Note); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// RetryAutomationAction is only for an explicitly reviewed failed attempt.
func (s *Store) RetryAutomationAction(profileID, key string) error {
	r, err := s.db.Exec(`DELETE FROM automation_attempts WHERE profile_id = ? AND action_key = ?
		AND state != 'submitted'`, normalizeProfileID(profileID), key)
	if err != nil {
		return err
	}
	n, err := r.RowsAffected()
	if err == nil && n == 0 {
		return sql.ErrNoRows
	}
	return err
}

// EnsureAutomationTask makes email-derived and catalog forms visible in the
// existing Tasks UI without creating another task each time an agent resumes.
func (s *Store) EnsureAutomationTask(profileID, brokerID, brokerName string, kind TaskType, rawURL string) (*PendingTask, error) {
	profileID = normalizeProfileID(profileID)
	_, err := s.db.Exec(`INSERT INTO pending_tasks
		(profile_id, broker_id, broker_name, task_type, form_url, notes, status, created_at)
		SELECT ?, ?, ?, ?, ?, 'Opened through MCP', 'pending', ? WHERE NOT EXISTS
		(SELECT 1 FROM pending_tasks WHERE profile_id = ? AND broker_id = ? AND form_url = ?)`,
		profileID, brokerID, brokerName, kind, rawURL, time.Now(), profileID, brokerID, rawURL)
	if err != nil {
		return nil, err
	}
	var id int64
	err = s.db.QueryRow(`SELECT id FROM pending_tasks WHERE profile_id = ? AND broker_id = ? AND form_url = ?
		ORDER BY id DESC LIMIT 1`, profileID, brokerID, rawURL).Scan(&id)
	if err != nil {
		return nil, err
	}
	return s.GetPendingTaskByID(id, profileID)
}

// UpdateAutomationTask keeps notes/evidence and completion in one DB update.
// A missing, wrong-profile, completed, or skipped task is an error.
func (s *Store) UpdateAutomationTask(profileID string, id int64, note, screenshot string, completed bool) error {
	status := "pending"
	var at any
	if completed {
		status, at = "completed", time.Now()
	}
	r, err := s.db.Exec(`UPDATE pending_tasks SET notes = ?, screenshot_path = ?, status = ?, completed_at = ?
		WHERE profile_id = ? AND id = ? AND status = 'pending'`, note, screenshot, status, at, normalizeProfileID(profileID), id)
	if err != nil {
		return err
	}
	n, err := r.RowsAffected()
	if err == nil && n == 0 {
		return sql.ErrNoRows
	}
	return err
}
