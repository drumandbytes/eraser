package web

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"
)

// JobStatus represents the status of a background job
type JobStatus string

const (
	JobStatusRunning   JobStatus = "running"
	JobStatusCompleted JobStatus = "completed"
	JobStatusCancelled JobStatus = "cancelled"
	JobStatusPaused    JobStatus = "paused" // Paused due to daily limit
	JobStatusError     JobStatus = "error"  // Stopped due to auth/config error
)

// Job represents a background email sending job
type Job struct {
	ID              string    `json:"id"`
	ProfileID       string    `json:"profile_id"`
	Status          JobStatus `json:"status"`
	Progress        int       `json:"progress"`
	Sent            int       `json:"sent"`
	Failed          int       `json:"failed"`
	Total           int       `json:"total"`
	CurrentBroker   string    `json:"current_broker"`
	CurrentBrokerID string    `json:"current_broker_id"`
	StartedAt       time.Time `json:"started_at"`
	CompletedAt     time.Time `json:"completed_at,omitempty"`
	Error           string    `json:"error,omitempty"`
	ErrorType       string    `json:"error_type,omitempty"`  // "auth", "rate_limit", etc.
	DailyLimit      int       `json:"daily_limit,omitempty"` // Max emails per day
	DaySent         int       `json:"day_sent,omitempty"`    // Emails sent today

	ctx                  context.Context
	cancelFunc           context.CancelFunc
	mu                   sync.Mutex
	consecutiveAuthFails int // Track consecutive auth failures
}

// Update updates the job progress. currentBrokerID is used by the brokers
// page to refresh only the one row that just changed (see
// internal/web/templates/brokers.html's refreshBrokerRow) instead of
// re-fetching the entire broker table on every poll tick.
func (j *Job) Update(sent, failed int, currentBroker, currentBrokerID string) {
	j.mu.Lock()
	defer j.mu.Unlock()

	j.Sent = sent
	j.Failed = failed
	j.CurrentBroker = currentBroker
	j.CurrentBrokerID = currentBrokerID
	if j.Total > 0 {
		j.Progress = ((sent + failed) * 100) / j.Total
	}
}

// Complete marks the job as completed
func (j *Job) Complete() {
	j.mu.Lock()
	defer j.mu.Unlock()

	j.Status = JobStatusCompleted
	j.CompletedAt = time.Now()
	j.Progress = 100
	j.CurrentBroker = ""
	j.CurrentBrokerID = ""
}

// StopWithError stops the job due to an error
func (j *Job) StopWithError(errorType, errorMsg string) {
	j.mu.Lock()
	defer j.mu.Unlock()

	j.Status = JobStatusCompleted
	j.CompletedAt = time.Now()
	j.Error = errorMsg
	j.ErrorType = errorType
	j.CurrentBroker = ""
	j.CurrentBrokerID = ""
}

// Pause marks the job paused (daily send limit reached) with the given
// day-sent count and message, all under one lock - processSendJob used to
// set these three fields directly, racing with any concurrent read (e.g. a
// status poll marshalling the job).
func (j *Job) Pause(daySent int, errorMsg string) {
	j.mu.Lock()
	defer j.mu.Unlock()

	j.DaySent = daySent
	j.Status = JobStatusPaused
	j.Error = errorMsg
}

// RecordAuthFailure records an auth failure and returns true if job should stop
func (j *Job) RecordAuthFailure() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.consecutiveAuthFails++
	return j.consecutiveAuthFails >= 3
}

// ResetAuthFailures resets the consecutive auth failure counter
func (j *Job) ResetAuthFailures() {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.consecutiveAuthFails = 0
}

// SetDailyLimit sets the job's daily send limit under lock.
func (j *Job) SetDailyLimit(limit int) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.DailyLimit = limit
}

// Cancel cancels the job
func (j *Job) Cancel() {
	j.mu.Lock()
	defer j.mu.Unlock()

	if j.Status == JobStatusRunning {
		j.Status = JobStatusCancelled
		j.CompletedAt = time.Now()
		if j.cancelFunc != nil {
			j.cancelFunc()
		}
	}
}

// IsCancelled returns true if the job was cancelled
func (j *Job) IsCancelled() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.Status == JobStatusCancelled
}

// GetStatus returns the job's current status. Status is mutated under j.mu
// by Update/Complete/Cancel/StopWithError, so reading the field directly
// (as GetActive and Cleanup used to) is a data race - go through this.
func (j *Job) GetStatus() JobStatus {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.Status
}

// finishedBefore reports whether the job is no longer running and finished
// before the given cutoff - the exact check JobManager.Cleanup needs,
// taken under j.mu so it can't race with a concurrent Update/Complete.
func (j *Job) finishedBefore(cutoff time.Time) bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.Status != JobStatusRunning && j.CompletedAt.Before(cutoff)
}

// Context returns the job's context
func (j *Job) Context() context.Context {
	return j.ctx
}

// MarshalJSON snapshots the job under the lock, so a status poll can't read a
// half-updated Job while processSendJob is mid-Update. The exported fields'
// json tags do the rest.
func (j *Job) MarshalJSON() ([]byte, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	type alias Job // no MarshalJSON method -> plain struct marshal, no recursion
	return json.Marshal((*alias)(j))
}

// JobManager manages background jobs
type JobManager struct {
	jobs map[string]*Job
	mu   sync.RWMutex
}

// jobRetention controls how long a completed job stays in memory before
// Cleanup evicts it. Jobs aren't persisted anywhere else, so this is purely
// about not letting `jm.jobs` grow unbounded across a long-running `serve`
// process - a week is plenty of time to check a completed send's results.
const jobRetention = 7 * 24 * time.Hour

// NewJobManager creates a new job manager and starts its background
// cleanup loop, which evicts completed jobs older than jobRetention.
func NewJobManager() *JobManager {
	jm := &JobManager{
		jobs: make(map[string]*Job),
	}
	go jm.cleanupLoop()
	return jm
}

// cleanupLoop periodically evicts old completed jobs so long-running
// `serve` processes don't accumulate job state forever.
func (jm *JobManager) cleanupLoop() {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()

	for range ticker.C {
		jm.Cleanup(jobRetention)
	}
}

// Create creates a new job with the given total count, scoped to profileID
func (jm *JobManager) Create(total int, profileID string) *Job {
	jm.mu.Lock()
	defer jm.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())

	job := &Job{
		ID:         uuid.New().String(),
		ProfileID:  profileID,
		Status:     JobStatusRunning,
		Progress:   0,
		Sent:       0,
		Failed:     0,
		Total:      total,
		StartedAt:  time.Now(),
		ctx:        ctx,
		cancelFunc: cancel,
	}

	jm.jobs[job.ID] = job
	return job
}

// Get returns a job by ID, or nil if not found
func (jm *JobManager) Get(id string) *Job {
	jm.mu.RLock()
	defer jm.mu.RUnlock()

	return jm.jobs[id]
}

// GetActive returns the currently running job for the given profile, or nil
// if none. Scoped per-profile so switching profiles in the web UI doesn't
// report a false "job already running" - two profiles can send
// concurrently, each against its own daily limit and history.
func (jm *JobManager) GetActive(profileID string) *Job {
	jm.mu.RLock()
	defer jm.mu.RUnlock()

	for _, job := range jm.jobs {
		if job.GetStatus() == JobStatusRunning && job.ProfileID == profileID {
			return job
		}
	}
	return nil
}

// Cleanup removes completed jobs older than the specified duration
func (jm *JobManager) Cleanup(maxAge time.Duration) {
	jm.mu.Lock()
	defer jm.mu.Unlock()

	cutoff := time.Now().Add(-maxAge)
	for id, job := range jm.jobs {
		if job.finishedBefore(cutoff) {
			delete(jm.jobs, id)
		}
	}
}

// PersistentJobState represents a job that can be saved/loaded from disk
type PersistentJobState struct {
	ID               string    `json:"id"`
	ProfileID        string    `json:"profile_id"`
	Status           JobStatus `json:"status"`
	Sent             int       `json:"sent"`
	Failed           int       `json:"failed"`
	Total            int       `json:"total"`
	StartedAt        time.Time `json:"started_at"`
	RemainingBrokers []string  `json:"remaining_brokers"` // Broker IDs still to process
	Search           string    `json:"search"`            // Original filter params
	Category         string    `json:"category"`
	Region           string    `json:"region"`
	StatusFilter     string    `json:"status_filter"`
}

// JobPersistence handles saving/loading job state
type JobPersistence struct {
	dataDir string
}

// NewJobPersistence creates a new job persistence handler
func NewJobPersistence(dataDir string) *JobPersistence {
	return &JobPersistence{dataDir: dataDir}
}

func (jp *JobPersistence) filePath() string {
	return filepath.Join(jp.dataDir, "pending_job.json")
}

// Save saves the job state to disk
func (jp *JobPersistence) Save(state *PersistentJobState) error {
	if err := os.MkdirAll(jp.dataDir, 0700); err != nil {
		return err
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(jp.filePath(), data, 0600)
}

// Load loads a pending job state from disk, returns nil if none exists
func (jp *JobPersistence) Load() (*PersistentJobState, error) {
	data, err := os.ReadFile(jp.filePath())
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var state PersistentJobState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, err
	}

	return &state, nil
}

// Clear removes the saved job state
func (jp *JobPersistence) Clear() error {
	err := os.Remove(jp.filePath())
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
