package history

import "fmt"

// pipelineRank orders stages so automated writers only move a request forward (a
// late "awaiting response" can't undo a confirmation). People use OverridePipelineStatus.
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

// UpdatePipelineStatus moves the latest request forward only; earlier or unknown stages are ignored.
func (s *Store) UpdatePipelineStatus(profileID, brokerID string, status PipelineStatus) error {
	return s.setPipelineStatus(profileID, brokerID, status, true, "")
}

// UpdatePipelineStatusBy records which agent client moved the stage, so the
// export can say who did it.
func (s *Store) UpdatePipelineStatusBy(profileID, brokerID string, status PipelineStatus, actor string) error {
	return s.setPipelineStatus(profileID, brokerID, status, true, actor)
}

// OverridePipelineStatus is for a person's explicit decision in the UI.
func (s *Store) OverridePipelineStatus(profileID, brokerID string, status PipelineStatus) error {
	return s.setPipelineStatus(profileID, brokerID, status, false, "human")
}

func (s *Store) setPipelineStatus(profileID, brokerID string, status PipelineStatus, forwardOnly bool, actor string) error {
	profileID = normalizeProfileID(profileID)
	guard := ""
	args := []any{status, actor, profileID, brokerID, profileID, brokerID}
	if forwardOnly {
		guard = fmt.Sprintf(" AND ? >= (%s)", rankCase)
		args = append(args, pipelineRank[status])
	}
	query := `UPDATE removal_requests SET pipeline_status = ?, pipeline_actor = ? WHERE profile_id = ? AND broker_id = ? AND id = (
		SELECT id FROM removal_requests WHERE profile_id = ? AND broker_id = ? ORDER BY sent_at DESC LIMIT 1
	)` + guard
	if _, err := s.db.Exec(query, args...); err != nil {
		return fmt.Errorf("failed to update pipeline status: %w", err)
	}
	return nil
}
