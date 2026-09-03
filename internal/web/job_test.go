package web

import (
	"encoding/json"
	"sync"
	"testing"
)

// jobSnap is the subset of a job's JSON a status poll checks.
type jobSnap struct {
	Sent            int    `json:"sent"`
	Failed          int    `json:"failed"`
	Progress        int    `json:"progress"`
	Total           int    `json:"total"`
	DailyLimit      int    `json:"daily_limit"`
	CurrentBroker   string `json:"current_broker"`
	CurrentBrokerID string `json:"current_broker_id"`
}

func readJob(t *testing.T, j *Job) jobSnap {
	t.Helper()
	b, err := json.Marshal(j) // locks via Job.MarshalJSON
	if err != nil {
		t.Fatalf("marshal job: %v", err)
	}
	var s jobSnap
	if err := json.Unmarshal(b, &s); err != nil {
		t.Fatalf("unmarshal job: %v", err)
	}
	return s
}

// TestJobConcurrentUpdateAndReadIsConsistent hammers Job.Update,
// Job.SetDailyLimit, and a status-poll marshal from many goroutines at once
// and checks every snapshot is internally consistent: Progress always matches
// the formula applied to that same snapshot's Sent/Failed/Total, never a value
// carried over from a different, interleaved call. This is the invariant that
// resumePendingJob/processSendJob used to violate by assigning
// job.Sent/job.Failed/job.Progress directly instead of going through the
// mutex-protected Update method. Run with `go test -race`.
func TestJobConcurrentUpdateAndReadIsConsistent(t *testing.T) {
	jm := NewJobManager()
	total := 100
	job := jm.Create(total, "profile-a")

	const goroutines = 16
	const iterations = 500

	var wg sync.WaitGroup

	// Writers: call Update with varying (sent, failed) pairs, exactly what
	// processSendJob does in its send loop.
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				sent := (g*iterations + i) % total
				failed := (total - sent) % total
				job.Update(sent, failed, "broker-x", "broker-x-id")
			}
		}(g)
	}

	// Also hammer SetDailyLimit concurrently, as processSendJob does once
	// per job but here repeated to stress the lock.
	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				job.SetDailyLimit(100 + g)
			}
		}(g)
	}

	// Readers: ToJSON, exactly what a status-polling request does.
	done := make(chan struct{})
	var readerWG sync.WaitGroup
	readerWG.Add(1)
	go func() {
		defer readerWG.Done()
		for {
			select {
			case <-done:
				return
			default:
			}

			snap := readJob(t, job)

			wantProgress := 0
			if snap.Total > 0 {
				wantProgress = ((snap.Sent + snap.Failed) * 100) / snap.Total
			}
			if snap.Progress != wantProgress {
				t.Errorf("torn snapshot: sent=%d failed=%d total=%d progress=%d, want progress=%d",
					snap.Sent, snap.Failed, snap.Total, snap.Progress, wantProgress)
				return
			}
		}
	}()

	wg.Wait()
	close(done)
	readerWG.Wait()
}

// TestJobUpdateSetsAllFieldsUnderOneLock is a narrower, deterministic
// companion to the concurrency test above: it checks that a single Update
// call always leaves Sent/Failed/Progress mutually consistent, and that
// SetDailyLimit only ever touches DailyLimit.
func TestJobUpdateSetsAllFieldsUnderOneLock(t *testing.T) {
	jm := NewJobManager()
	job := jm.Create(10, "profile-a")

	job.Update(3, 2, "broker-y", "broker-y-id")
	snap := readJob(t, job)
	if snap.Sent != 3 || snap.Failed != 2 || snap.Progress != 50 { // (3+2)*100/10
		t.Errorf("after Update: %+v", snap)
	}
	if snap.CurrentBroker != "broker-y" || snap.CurrentBrokerID != "broker-y-id" {
		t.Errorf("after Update: %+v", snap)
	}

	job.SetDailyLimit(42)
	snap = readJob(t, job)
	if snap.DailyLimit != 42 {
		t.Errorf("daily_limit = %d, want 42", snap.DailyLimit)
	}
	// SetDailyLimit must not have disturbed Sent/Failed/Progress.
	if snap.Sent != 3 || snap.Progress != 50 {
		t.Errorf("SetDailyLimit disturbed progress fields: %+v", snap)
	}
}

// TestJobManagerCreateAndGetIgnoresProfile confirms that JobManager.Get
// itself is not profile-scoped - it happily returns a job regardless of
// which profile is asking. This documents why the scoping check has to live
// in the HTTP handlers (see handlers_jobs_test.go), not in JobManager.
func TestJobManagerCreateAndGetIgnoresProfile(t *testing.T) {
	jm := NewJobManager()
	jobA := jm.Create(5, "profile-a")
	jobB := jm.Create(5, "profile-b")

	if got := jm.Get(jobA.ID); got != jobA {
		t.Fatalf("Get(jobA.ID) = %v, want jobA", got)
	}
	if got := jm.Get(jobB.ID); got != jobB {
		t.Fatalf("Get(jobB.ID) = %v, want jobB", got)
	}

	// JobManager.Get does not filter by profile - a caller asking for
	// jobB's ID gets jobB back even though it belongs to a different
	// profile than jobA. Profile scoping is the handler's job.
	if got := jm.Get(jobB.ID); got.ProfileID != "profile-b" {
		t.Fatalf("jobB.ProfileID = %q, want %q", got.ProfileID, "profile-b")
	}
}
