package history

import (
	"testing"
	"time"
)

func TestBatchApprovalIsSingleUseAndDigestBound(t *testing.T) {
	s := newTestStore(t)
	if err := s.CreateBatchApproval(BatchApproval{PlanID: "p1", ProfileID: "default", Digest: "d1", Preview: "{}", ExpiresAt: time.Now().Add(time.Minute)}); err != nil {
		t.Fatal(err)
	}
	if err := s.ConsumeBatchApproval("p1", "d1"); err == nil {
		t.Fatal("consumed before approval")
	}
	if got, _ := s.PendingBatchApprovals(); len(got) != 1 || got[0].PlanID != "p1" {
		t.Fatalf("pending=%+v", got)
	}
	if err := s.ApproveBatch("p1", "cli"); err != nil {
		t.Fatal(err)
	}
	if err := s.ApproveBatch("p1", "web"); err == nil {
		t.Fatal("approved twice")
	}
	if err := s.ConsumeBatchApproval("p1", "other-digest"); err == nil {
		t.Fatal("consumed with a different digest")
	}
	if err := s.ConsumeBatchApproval("p1", "d1"); err != nil {
		t.Fatal(err)
	}
	if err := s.ConsumeBatchApproval("p1", "d1"); err == nil {
		t.Fatal("spent approval consumed again")
	}
}

func TestBatchApprovalExpires(t *testing.T) {
	s := newTestStore(t)
	if err := s.CreateBatchApproval(BatchApproval{PlanID: "old", ProfileID: "default", Digest: "d", Preview: "{}", ExpiresAt: time.Now().Add(-time.Second)}); err != nil {
		t.Fatal(err)
	}
	if err := s.ApproveBatch("old", "web"); err == nil {
		t.Fatal("expired preview approved")
	}
	if got, _ := s.PendingBatchApprovals(); len(got) != 0 {
		t.Fatalf("expired preview listed: %+v", got)
	}
}
