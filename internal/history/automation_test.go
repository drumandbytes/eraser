package history

import (
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
)

func TestAutomationClaimsSurviveRestartAndIsolateProfiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.db")
	a, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.ClaimAutomationAction("alice", "first", "form"); err != nil {
		t.Fatal(err)
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	b, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = b.Close() }()
	if err := b.ClaimAutomationAction("alice", "second", "form"); err == nil {
		t.Fatal("replayed an uncertain action after restart")
	}
	if err := b.ClaimAutomationAction("bob", "first", "form"); err != nil {
		t.Fatal(err)
	}
	if err := b.RetryAutomationAction("alice", "missing"); err == nil {
		t.Fatal("missing attempt accepted")
	}
	if err := b.FinishAutomationAction("bob", "first", "submitted", "accepted"); err != nil {
		t.Fatal(err)
	}
	if err := b.RetryAutomationAction("bob", "first"); err == nil {
		t.Fatal("submitted action made retryable")
	}
	if err := b.RetryAutomationAction("alice", "first"); err != nil {
		t.Fatal(err)
	}
	if err := b.ClaimAutomationAction("alice", "first", "form"); err != nil {
		t.Fatal(err)
	}
}

func TestAutomationClaimIsAtomic(t *testing.T) {
	s, err := NewStore(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	var wins atomic.Int32
	var wg sync.WaitGroup
	for range 10 {
		wg.Go(func() {
			if s.ClaimAutomationAction("alice", "submit", "form") == nil {
				wins.Add(1)
			}
		})
	}
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatalf("claims = %d, want 1", wins.Load())
	}
}

func TestAutomationTaskVisibleAndProfileScoped(t *testing.T) {
	s, err := NewStore(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	task, err := s.EnsureAutomationTask("alice", "broker", "Broker", TaskManualForm, "https://example.com/form")
	if err != nil {
		t.Fatal(err)
	}
	again, err := s.EnsureAutomationTask("alice", "broker", "Broker", TaskManualForm, "https://example.com/form")
	if err != nil || again.ID != task.ID {
		t.Fatalf("duplicate task: %v", err)
	}
	if err := s.UpdateAutomationTask("bob", task.ID, "accepted", "proof.png", true); err == nil {
		t.Fatal("cross-profile write succeeded")
	}
	if err := s.UpdateAutomationTask("alice", task.ID, "Request accepted; awaiting reply", "proof.png", true); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetPendingTasks("alice", "", "completed")
	if err != nil || len(got) != 1 || got[0].ScreenshotPath != "proof.png" {
		t.Fatalf("UI task data: %+v, %v", got, err)
	}
	if err := s.UpdateAutomationTask("alice", task.ID, "overwrite", "", false); err == nil {
		t.Fatal("reopened completed task")
	}
}
