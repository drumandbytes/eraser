package history

import (
	"database/sql"
	"fmt"
	"time"
)

// NoDataClassifier keeps database migrations independent of the inbox package.
// It returns whether the saved reply says no data, its confidence, and whether
// that conclusion still needs a person's review.
type NoDataClassifier func(BrokerResponse) (bool, float64, bool)

// MigrateNoData reclassifies saved replies once, atomically across profiles.
// Claiming the version inside the write transaction also serializes two app
// processes upgrading the same database. A failed migration is retryable.
func (s *Store) MigrateNoData(classify NoDataClassifier) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS data_migrations (name TEXT PRIMARY KEY, applied_at DATETIME NOT NULL)`); err != nil {
		return err
	}
	result, err := tx.Exec(`INSERT INTO data_migrations (name, applied_at) VALUES ('no_data_v1', ?) ON CONFLICT(name) DO NOTHING`, time.Now())
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return nil
	}

	rows, err := tx.Query(`SELECT ` + brokerResponseCols + ` FROM broker_responses ORDER BY id`)
	if err != nil {
		return err
	}
	var replies []BrokerResponse
	for rows.Next() {
		r, err := scanBrokerResponse(rows)
		if err != nil {
			_ = rows.Close()
			return err
		}
		replies = append(replies, *r)
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return err
	}
	resolved := make(map[[2]string]bool)
	for _, r := range replies {
		matched, confidence, review := classify(r)
		if !matched {
			continue
		}
		if _, err := tx.Exec(`UPDATE broker_responses SET response_type = 'no_data', confidence = ?, needs_review = ? WHERE id = ? AND profile_id = ?`, confidence, review, r.ID, r.ProfileID); err != nil {
			return err
		}
		if !review && confidence >= 0.7 {
			resolved[[2]string{r.ProfileID, r.BrokerID}] = true
		}
	}
	for key := range resolved {
		if err := resolveNoData(tx, key[0], key[1], true); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ResolveNoData reconciles only a confident, latest no-data reply. Call after
// inserting or reclassifying a reply; repeated calls are safe.
func (s *Store) ResolveNoData(profileID, brokerID string, reclassified bool) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	// Obtain the writer lock before reading, avoiding a WAL read/write upgrade.
	if _, err := tx.Exec(`UPDATE broker_responses SET response_type = response_type WHERE profile_id = ? AND broker_id = ? AND response_type = 'no_data'`, normalizeProfileID(profileID), brokerID); err != nil {
		return err
	}
	if err := resolveNoData(tx, normalizeProfileID(profileID), brokerID, reclassified); err != nil {
		return err
	}
	return tx.Commit()
}

func resolveNoData(tx *sql.Tx, profileID, brokerID string, upgrading bool) error {
	replyRows, err := tx.Query(`SELECT `+brokerResponseCols+` FROM broker_responses WHERE profile_id = ? AND broker_id = ?`, profileID, brokerID)
	if err != nil {
		return err
	}
	var r *BrokerResponse
	for replyRows.Next() {
		candidate, err := scanBrokerResponse(replyRows)
		if err != nil {
			_ = replyRows.Close()
			return err
		}
		if r == nil || candidate.ReceivedAt.After(r.ReceivedAt) || (candidate.ReceivedAt.Equal(r.ReceivedAt) && candidate.ID > r.ID) {
			r = candidate
		}
	}
	err = replyRows.Err()
	_ = replyRows.Close()
	if err != nil {
		return err
	}
	if r == nil || r.ResponseType != "no_data" || r.NeedsReview || r.Confidence < 0.7 || r.ReceivedAt.IsZero() {
		return nil
	}
	var requestID int64
	var sentAt sql.NullTime
	var status sql.NullString
	requestRows, err := tx.Query(`SELECT id, sent_at, pipeline_status FROM removal_requests WHERE profile_id = ? AND broker_id = ?`, profileID, brokerID)
	if err != nil {
		return err
	}
	for requestRows.Next() {
		var id int64
		var sent sql.NullTime
		var stage sql.NullString
		if err := requestRows.Scan(&id, &sent, &stage); err != nil {
			_ = requestRows.Close()
			return err
		}
		if requestID == 0 || sent.Time.After(sentAt.Time) || (sent.Time.Equal(sentAt.Time) && id > requestID) {
			requestID, sentAt, status = id, sent, stage
		}
	}
	err = requestRows.Err()
	_ = requestRows.Close()
	if err != nil {
		return err
	}
	if !sentAt.Valid || r.ReceivedAt.Before(sentAt.Time) {
		return nil
	}

	// There is no pipeline updated_at in legacy databases. On upgrade, only
	// replace the old classifier's no-record outcomes; preserve manual progress.
	switch PipelineStatus(status.String) {
	case "", PipelineEmailSent, PipelineAwaitingResponse, PipelineRejected, PipelineNoData:
	case PipelineFormRequired, PipelineAwaitingConfirmation, PipelineAwaitingCaptcha:
		if upgrading {
			return nil
		}
	default:
		return nil
	}
	// SQLite cannot compare every legacy Go timestamp representation with
	// julianday(). Read typed times and compare instants, including time zones.
	rows, err := tx.Query(`SELECT id, status, created_at, completed_at FROM pending_tasks WHERE profile_id = ? AND broker_id = ?`, profileID, brokerID)
	if err != nil {
		return err
	}
	var superseded []int64
	later := false
	for rows.Next() {
		var id int64
		var taskStatus string
		var created, completed sql.NullTime
		if err := rows.Scan(&id, &taskStatus, &created, &completed); err != nil {
			_ = rows.Close()
			return err
		}
		if created.Time.After(r.ReceivedAt) || (completed.Valid && completed.Time.After(r.ReceivedAt)) {
			later = true
		}
		if taskStatus == "pending" && created.Valid && !created.Time.Before(sentAt.Time) && !created.Time.After(r.ReceivedAt) {
			superseded = append(superseded, id)
		}
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return err
	}
	if later {
		return nil
	}
	if _, err := tx.Exec(`UPDATE removal_requests SET pipeline_status = 'no_data' WHERE id = ? AND profile_id = ?`, requestID, profileID); err != nil {
		return err
	}
	// Keep the work and its notes as history; don't pretend a form was submitted.
	for _, id := range superseded {
		if _, err := tx.Exec(`UPDATE pending_tasks SET status = 'completed', completed_at = ?, notes =
            CASE WHEN COALESCE(notes, '') = '' THEN 'Resolved: broker reports no data held.' ELSE notes || char(10) || 'Resolved: broker reports no data held.' END
            WHERE id = ? AND profile_id = ? AND status = 'pending'`, r.ReceivedAt.UTC(), id, profileID); err != nil {
			return err
		}
	}
	rows, err = tx.Query(`SELECT `+brokerResponseCols+` FROM broker_responses WHERE profile_id = ? AND broker_id = ? AND needs_review = 1`, profileID, brokerID)
	if err != nil {
		return err
	}
	var reviews []int64
	for rows.Next() {
		old, err := scanBrokerResponse(rows)
		if err != nil {
			_ = rows.Close()
			return err
		}
		if !old.ReceivedAt.Before(sentAt.Time) && old.ReceivedAt.Before(r.ReceivedAt) {
			reviews = append(reviews, old.ID)
		}
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return err
	}
	for _, id := range reviews {
		if _, err := tx.Exec(`UPDATE broker_responses SET needs_review = 0 WHERE id = ? AND profile_id = ?`, id, profileID); err != nil {
			return fmt.Errorf("resolve superseded response reviews: %w", err)
		}
	}
	return nil
}
