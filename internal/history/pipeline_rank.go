package history

import "fmt"

// pipelineRank orders stages so automated writers only move a request forward.
// A late "awaiting response" cannot overwrite a confirmation. Human decisions
// use OverridePipelineStatus instead.
var pipelineRank = map[PipelineStatus]int{
	PipelineEmailSent:            1,
	PipelineAwaitingResponse:     2,
	PipelineFormRequired:         3,
	PipelineAwaitingCaptcha:      4,
	PipelineCaptchaSolved:        5,
	PipelineFormFilled:           6,
	PipelineAwaitingConfirmation: 7,
	PipelineRejected:             8,
	PipelineNoData:               8,
	PipelineFailed:               8,
	PipelineConfirmed:            9,
}

// rankCase is the SQL form of pipelineRank for the status already stored.
var rankCase = func() string {
	s := "CASE pipeline_status"
	for status, rank := range pipelineRank {
		s += fmt.Sprintf(" WHEN '%s' THEN %d", status, rank)
	}
	return s + " ELSE 0 END"
}()

// UpdatePipelineStatus moves the latest request forward only. Unknown or
// earlier stages are ignored, so a late automated reply cannot undo progress.
func (s *Store) UpdatePipelineStatus(profileID, brokerID string, status PipelineStatus) error {
	return s.setPipelineStatus(profileID, brokerID, status, true)
}

// OverridePipelineStatus is for a person's explicit decision in the UI.
func (s *Store) OverridePipelineStatus(profileID, brokerID string, status PipelineStatus) error {
	return s.setPipelineStatus(profileID, brokerID, status, false)
}

func (s *Store) setPipelineStatus(profileID, brokerID string, status PipelineStatus, forwardOnly bool) error {
	profileID = normalizeProfileID(profileID)
	guard := ""
	args := []any{status, profileID, brokerID, profileID, brokerID}
	if forwardOnly {
		guard = fmt.Sprintf(" AND ? >= (%s)", rankCase)
		args = append(args, pipelineRank[status])
	}
	query := `UPDATE removal_requests SET pipeline_status = ? WHERE profile_id = ? AND broker_id = ? AND id = (
		SELECT id FROM removal_requests WHERE profile_id = ? AND broker_id = ? ORDER BY sent_at DESC LIMIT 1
	)` + guard
	if _, err := s.db.Exec(query, args...); err != nil {
		return fmt.Errorf("failed to update pipeline status: %w", err)
	}
	return nil
}
