package history

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// BatchApproval is an MCP batch preview waiting for a person. It's stored so the
// approver sees what the digest covers.
type BatchApproval struct {
	PlanID     string
	ProfileID  string
	Digest     string
	Preview    string
	ExpiresAt  time.Time
	ApprovedAt sql.NullTime
	ApprovedBy string
}

// ErrNoPendingApproval: unknown, expired, already approved or already spent.
var ErrNoPendingApproval = errors.New("no pending approval for this batch")

func (s *Store) migrateApprovals() error {
	_, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS batch_approvals (
		plan_id TEXT PRIMARY KEY, profile_id TEXT NOT NULL, digest TEXT NOT NULL,
		preview TEXT NOT NULL, created_at DATETIME NOT NULL, expires_at DATETIME NOT NULL,
		approved_at DATETIME, approved_by TEXT NOT NULL DEFAULT '', consumed_at DATETIME
	)`)
	return err
}

func (s *Store) CreateBatchApproval(a BatchApproval) error {
	_, err := s.db.Exec(`INSERT INTO batch_approvals (plan_id, profile_id, digest, preview, created_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?)`, a.PlanID, normalizeProfileID(a.ProfileID), a.Digest, a.Preview, time.Now(), a.ExpiresAt)
	return err
}

// ApproveBatch is the web UI / `eraser mcp approve` side: pending and unexpired only, once.
func (s *Store) ApproveBatch(planID, approver string) error {
	r, err := s.db.Exec(`UPDATE batch_approvals SET approved_at = ?, approved_by = ?
		WHERE plan_id = ? AND approved_at IS NULL AND consumed_at IS NULL AND expires_at > ?`,
		time.Now(), approver, planID, time.Now())
	if err != nil {
		return err
	}
	if n, _ := r.RowsAffected(); n == 0 {
		return ErrNoPendingApproval
	}
	return nil
}

// ConsumeBatchApproval spends an approval for the exact digest. Atomic: one approval, one batch.
func (s *Store) ConsumeBatchApproval(planID, digest string) error {
	r, err := s.db.Exec(`UPDATE batch_approvals SET consumed_at = ?
		WHERE plan_id = ? AND digest = ? AND approved_at IS NOT NULL AND consumed_at IS NULL AND expires_at > ?`,
		time.Now(), planID, digest, time.Now())
	if err != nil {
		return fmt.Errorf("consume batch approval: %w", err)
	}
	if n, _ := r.RowsAffected(); n == 0 {
		return ErrNoPendingApproval
	}
	return nil
}

func (s *Store) PendingBatchApprovals() ([]BatchApproval, error) {
	rows, err := s.db.Query(`SELECT plan_id, profile_id, digest, preview, expires_at FROM batch_approvals
		WHERE approved_at IS NULL AND consumed_at IS NULL AND expires_at > ? ORDER BY created_at`, time.Now())
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []BatchApproval
	for rows.Next() {
		var a BatchApproval
		if err := rows.Scan(&a.PlanID, &a.ProfileID, &a.Digest, &a.Preview, &a.ExpiresAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
