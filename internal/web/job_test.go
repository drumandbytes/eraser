package web

import (
	"encoding/json"
	"os"
	"path/filepath"
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

// TestCreateIfNoActiveRejectsSecondJobForSameProfile is the sequential case:
// once a profile has a running job, a second CreateIfNoActive for that
// profile must return the existing job rather than creating another one.
func TestCreateIfNoActiveRejectsSecondJobForSameProfile(t *testing.T) {
	jm := NewJobManager()

	first, ok := jm.CreateIfNoActive(5, "profile-a")
	if !ok {
		t.Fatal("first CreateIfNoActive should have created a job")
	}

	second, ok := jm.CreateIfNoActive(5, "profile-a")
	if ok {
		t.Fatal("second CreateIfNoActive for the same active profile should not create a job")
	}
	if second != first {
		t.Fatalf("expected the existing active job back, got a different one")
	}

	// A different profile is unaffected.
	if _, ok := jm.CreateIfNoActive(5, "profile-b"); !ok {
		t.Fatal("CreateIfNoActive for a different profile should succeed")
	}

	// Once the first job finishes, the profile can start another.
	first.Complete()
	if _, ok := jm.CreateIfNoActive(5, "profile-a"); !ok {
		t.Fatal("CreateIfNoActive should succeed once the prior job for the profile has completed")
	}
}

// TestCreateIfNoActiveIsRaceSafe is the concurrent case CreateIfNoActive
// exists for: handleAPISendAll's separate GetActive-then-Create used to
// leave a window where two simultaneous requests for the same profile could
// both pass the check and each create their own job, double-sending every
// broker. Run with -race.
func TestCreateIfNoActiveIsRaceSafe(t *testing.T) {
	jm := NewJobManager()

	const attempts = 50
	var wg sync.WaitGroup
	created := make([]bool, attempts)
	for i := range attempts {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, ok := jm.CreateIfNoActive(1, "profile-a")
			created[i] = ok
		}(i)
	}
	wg.Wait()

	wins := 0
	for _, ok := range created {
		if ok {
			wins++
		}
	}
	if wins != 1 {
		t.Fatalf("expected exactly 1 of %d concurrent CreateIfNoActive calls to win, got %d", attempts, wins)
	}
}

// TestJobPersistencePerProfileFiles is the collision JobPersistence exists
// to avoid: two profiles saving concurrently used to share one
// pending_job.json, so the second Save silently overwrote the first and a
// restart could only ever resume (or forget) whichever wrote last.
func TestJobPersistencePerProfileFiles(t *testing.T) {
	jp := NewJobPersistence(t.TempDir())

	stateA := &PersistentJobState{ID: "job-a", ProfileID: "profile-a", Total: 3, RemainingBrokers: []string{"x"}}
	stateB := &PersistentJobState{ID: "job-b", ProfileID: "profile-b", Total: 5, RemainingBrokers: []string{"y", "z"}}

	if err := jp.Save(stateA); err != nil {
		t.Fatalf("Save(profile-a): %v", err)
	}
	if err := jp.Save(stateB); err != nil {
		t.Fatalf("Save(profile-b): %v", err)
	}

	gotA, err := jp.Load("profile-a")
	if err != nil || gotA == nil || gotA.ID != "job-a" {
		t.Fatalf("Load(profile-a) = %+v, %v, want job-a", gotA, err)
	}
	gotB, err := jp.Load("profile-b")
	if err != nil || gotB == nil || gotB.ID != "job-b" {
		t.Fatalf("Load(profile-b) = %+v, %v, want job-b", gotB, err)
	}

	// Clearing one profile's state doesn't touch the other's.
	if err := jp.Clear("profile-a"); err != nil {
		t.Fatalf("Clear(profile-a): %v", err)
	}
	if got, err := jp.Load("profile-a"); err != nil || got != nil {
		t.Fatalf("Load(profile-a) after Clear = %+v, %v, want nil", got, err)
	}
	if got, err := jp.Load("profile-b"); err != nil || got == nil {
		t.Fatalf("Load(profile-b) after clearing profile-a = %+v, %v, want job-b still present", got, err)
	}
}

// TestJobPersistenceDefaultProfileUsesLegacyFilename ensures a pending job
// saved before multi-profile support existed (or for the sole default
// profile most installs have) survives this upgrade: it must still be
// readable at the old bare pending_job.json path, not a
// pending_job-default.json this code never wrote before.
func TestJobPersistenceDefaultProfileUsesLegacyFilename(t *testing.T) {
	dir := t.TempDir()
	jp := NewJobPersistence(dir)

	if err := jp.Save(&PersistentJobState{ID: "job-legacy", ProfileID: "default", Total: 1}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "pending_job.json")); err != nil {
		t.Fatalf("expected legacy pending_job.json to exist: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "pending_job-default.json")); !os.IsNotExist(err) {
		t.Fatalf("expected no pending_job-default.json, got err=%v", err)
	}
}
