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

// Regression coverage for digisamroc/eraser#3: broker_responses.email_body
// was referenced by AddBrokerResponse's INSERT (and UpdateBrokerResponseBody,
// GetAllBrokerResponses, GetBrokerResponses) but missing from the CREATE
// TABLE, so `eraser monitor` failed on every classified reply with "table
// broker_responses has no column named email_body" on any database created
// before the migrate() fix. This exercises the exact path that broke.
func TestAddBrokerResponse_EmailBodyColumn(t *testing.T) {
	s := newTestStore(t)

	resp := &BrokerResponse{
		BrokerID:     "broker-a",
		BrokerName:   "Broker A",
		ResponseType: "pending",
		EmailFrom:    "privacy@broker-a.example.com",
		EmailSubject: "Re: Personal Data Removal Request",
		EmailBody:    "We have received your request and will respond within 30 days.",
		Confidence:   0.9,
	}
	if err := s.AddBrokerResponse(resp); err != nil {
		t.Fatalf("AddBrokerResponse: %v", err)
	}
	if resp.ID == 0 {
		t.Fatal("AddBrokerResponse did not set an ID")
	}

	all, err := s.GetAllBrokerResponses()
	if err != nil {
		t.Fatalf("GetAllBrokerResponses: %v", err)
	}
	if len(all) != 1 || all[0].EmailBody != resp.EmailBody {
		t.Fatalf("expected the stored email_body to round-trip, got %+v", all)
	}

	if err := s.UpdateBrokerResponseBody(resp.ID, "updated body"); err != nil {
		t.Fatalf("UpdateBrokerResponseBody: %v", err)
	}
}
