package history

import (
	"path/filepath"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "history.db")
	s, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func addRecord(t *testing.T, s *Store, brokerID string, status Status, sentAt time.Time) {
	t.Helper()
	rec := &Record{
		BrokerID:   brokerID,
		BrokerName: brokerID,
		Email:      brokerID + "@example.com",
		Template:   "gdpr",
		Status:     status,
		SentAt:     sentAt,
	}
	if err := s.Add(rec); err != nil {
		t.Fatalf("Add: %v", err)
	}
}

func TestCountSentSince(t *testing.T) {
	s := newTestStore(t)
	now := time.Now()

	addRecord(t, s, "broker-a", StatusSent, now.Add(-1*time.Hour))
	addRecord(t, s, "broker-b", StatusSent, now.Add(-30*time.Hour)) // outside 24h window
	addRecord(t, s, "broker-c", StatusFailed, now.Add(-1*time.Hour))

	count, err := s.CountSentSince(now.Add(-24 * time.Hour))
	if err != nil {
		t.Fatalf("CountSentSince: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 sent in last 24h, got %d", count)
	}
}

func TestLastSuccessfulSendTimes(t *testing.T) {
	s := newTestStore(t)
	now := time.Now()

	// Two sends for the same broker - should return the more recent one.
	addRecord(t, s, "broker-a", StatusSent, now.Add(-40*24*time.Hour))
	addRecord(t, s, "broker-a", StatusSent, now.Add(-2*24*time.Hour))
	addRecord(t, s, "broker-b", StatusFailed, now.Add(-1*time.Hour))

	times, err := s.LastSuccessfulSendTimes()
	if err != nil {
		t.Fatalf("LastSuccessfulSendTimes: %v", err)
	}

	if _, ok := times["broker-b"]; ok {
		t.Errorf("broker-b never had a successful send, should not appear")
	}

	got, ok := times["broker-a"]
	if !ok {
		t.Fatalf("expected broker-a in results")
	}
	wantApprox := now.Add(-2 * 24 * time.Hour)
	if got.Before(wantApprox.Add(-time.Minute)) || got.After(wantApprox.Add(time.Minute)) {
		t.Errorf("expected broker-a's last send near %v, got %v", wantApprox, got)
	}
}

func TestMarkFailedRemovesFromResendCooldown(t *testing.T) {
	s := newTestStore(t)
	now := time.Now()

	addRecord(t, s, "broker-a", StatusSent, now.Add(-1*time.Hour))

	n, err := s.MarkFailed("broker-a", "bounced - manually confirmed")
	if err != nil {
		t.Fatalf("MarkFailed: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 row updated, got %d", n)
	}

	times, err := s.LastSuccessfulSendTimes()
	if err != nil {
		t.Fatalf("LastSuccessfulSendTimes: %v", err)
	}
	if _, ok := times["broker-a"]; ok {
		t.Errorf("broker-a was just marked failed, should no longer count as a successful send")
	}
}

func TestMarkFailedNoRecordReturnsZero(t *testing.T) {
	s := newTestStore(t)

	n, err := s.MarkFailed("never-sent", "bounced")
	if err != nil {
		t.Fatalf("MarkFailed: %v", err)
	}
	if n != 0 {
		t.Errorf("expected 0 rows updated for a broker with no sent record, got %d", n)
	}
}

func TestMarkFailedOnlyTouchesMostRecentSentRecord(t *testing.T) {
	s := newTestStore(t)
	now := time.Now()

	addRecord(t, s, "broker-a", StatusSent, now.Add(-40*24*time.Hour))
	addRecord(t, s, "broker-a", StatusSent, now.Add(-1*time.Hour))

	if _, err := s.MarkFailed("broker-a", "bounced"); err != nil {
		t.Fatalf("MarkFailed: %v", err)
	}

	records, err := s.GetRecentRequests(10)
	if err != nil {
		t.Fatalf("GetRecentRequests: %v", err)
	}
	sentCount, failedCount := 0, 0
	for _, r := range records {
		if r.BrokerID != "broker-a" {
			continue
		}
		switch r.Status {
		case StatusSent:
			sentCount++
		case StatusFailed:
			failedCount++
		}
	}
	if sentCount != 1 || failedCount != 1 {
		t.Errorf("expected exactly one older 'sent' record to survive and one 'failed', got sent=%d failed=%d", sentCount, failedCount)
	}
}
