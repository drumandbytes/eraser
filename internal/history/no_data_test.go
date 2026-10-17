package history

import (
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestNoDataMigrationPreservesNewerWorkAndProfiles(t *testing.T) {
	for _, tc := range []struct {
		name                                       string
		stage                                      PipelineStatus
		newRequest, newReply, laterTask, uncertain bool
	}{
		{"old rejection", PipelineRejected, false, false, false, false},
		{"new request", PipelineRejected, true, false, false, false},
		{"new reply", PipelineAwaitingResponse, false, true, false, false},
		{"manual progress", PipelineFormFilled, false, false, false, false},
		{"confirmed", PipelineConfirmed, false, false, false, false},
		{"new task", PipelineRejected, false, false, true, false},
		{"uncertain", PipelineRejected, false, false, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestStore(t)
			sent := time.Now().Add(-48 * time.Hour)
			received := sent.Add(time.Hour)
			for _, p := range []string{"a", "b"} {
				if err := s.Add(&Record{ProfileID: p, BrokerID: "acme", BrokerName: "Acme", Status: StatusSent, SentAt: sent}); err != nil {
					t.Fatal(err)
				}
				if err := s.UpdatePipelineStatus(p, "acme", tc.stage); err != nil {
					t.Fatal(err)
				}
			}
			r := BrokerResponse{ProfileID: "a", BrokerID: "acme", BrokerName: "Acme", ResponseType: "rejected", EmailSubject: "Reply", EmailBody: "Original no-records body", ReceivedAt: received}
			if err := s.AddBrokerResponse(&r); err != nil {
				t.Fatal(err)
			}
			task := PendingTask{ProfileID: "a", BrokerID: "acme", BrokerName: "Acme", TaskType: TaskReview, Notes: "Original note"}
			if err := s.AddPendingTask(&task); err != nil {
				t.Fatal(err)
			}
			created := sent.Add(30 * time.Minute)
			if tc.laterTask {
				created = received.Add(time.Hour)
			}
			if _, err := s.db.Exec(`UPDATE pending_tasks SET created_at = ? WHERE id = ?`, created, task.ID); err != nil {
				t.Fatal(err)
			}
			if tc.newRequest {
				if err := s.Add(&Record{ProfileID: "a", BrokerID: "acme", BrokerName: "Acme", Status: StatusSent, SentAt: received.Add(time.Hour)}); err != nil {
					t.Fatal(err)
				}
			}
			if tc.newReply {
				if err := s.AddBrokerResponse(&BrokerResponse{ProfileID: "a", BrokerID: "acme", ResponseType: "pending", ReceivedAt: received.Add(time.Hour)}); err != nil {
					t.Fatal(err)
				}
			}
			classify := func(r BrokerResponse) (bool, float64, bool) { return r.EmailBody != "", 0.85, tc.uncertain }
			if err := s.MigrateNoData(classify); err != nil {
				t.Fatal(err)
			}
			got, _ := s.GetBrokerResponseByID(r.ID, "a")
			if got.ResponseType != "no_data_held" || got.EmailBody != r.EmailBody || got.NeedsReview != tc.uncertain {
				t.Fatalf("reply=%+v", got)
			}
			a, _ := s.LatestRequests("a")
			b, _ := s.LatestRequests("b")
			// The migration only relabels replies; it never moves requests or tasks.
			wantA := tc.stage
			if tc.newRequest {
				wantA = PipelineEmailSent
			}
			if a[0].PipelineStatus != wantA || b[0].PipelineStatus != tc.stage {
				t.Fatalf("a=%s want=%s; b=%s want=%s", a[0].PipelineStatus, wantA, b[0].PipelineStatus, tc.stage)
			}
			gotTask, _ := s.GetPendingTaskByID(task.ID, "a")
			if gotTask.Status != "pending" || strings.Contains(gotTask.Notes, "no data held") {
				t.Fatalf("migration changed task: %+v", gotTask)
			}
			if err := s.MigrateNoData(func(BrokerResponse) (bool, float64, bool) { t.Fatal("migration ran twice"); return false, 0, false }); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestNoDataMigrationRollsBackAndRetries(t *testing.T) {
	s := newTestStore(t)
	r := BrokerResponse{ProfileID: "a", BrokerID: "acme", ResponseType: "rejected", EmailBody: "Raw body", ReceivedAt: time.Now()}
	if err := s.AddBrokerResponse(&r); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`CREATE TRIGGER stop_upgrade BEFORE UPDATE OF response_type ON broker_responses BEGIN SELECT RAISE(ABORT, 'test rollback'); END`); err != nil {
		t.Fatal(err)
	}
	classify := func(BrokerResponse) (bool, float64, bool) { return true, 0.85, false }
	if err := s.MigrateNoData(classify); err == nil {
		t.Fatal("expected migration failure")
	}
	got, _ := s.GetBrokerResponseByID(r.ID, "a")
	if got.ResponseType != "rejected" {
		t.Fatal("partial migration committed")
	}
	if _, err := s.db.Exec(`DROP TRIGGER stop_upgrade`); err != nil {
		t.Fatal(err)
	}
	if err := s.MigrateNoData(classify); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetBrokerResponseByID(r.ID, "a")
	if got.ResponseType != "no_data_held" {
		t.Fatal("failed migration was not retried")
	}
}

func TestNoDataMigrationConcurrentStores(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.db")
	a, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = a.Close() }()
	b, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = b.Close() }()
	if err := a.AddBrokerResponse(&BrokerResponse{BrokerID: "acme", ResponseType: "rejected", ReceivedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	classify := func(BrokerResponse) (bool, float64, bool) { calls.Add(1); return true, 0.85, false }
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, s := range []*Store{a, b} {
		go func() { <-start; results <- s.MigrateNoData(classify) }()
	}
	close(start)
	for range 2 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("classifier ran %d times", calls.Load())
	}
}

func TestNoDataResolutionUsesInstantsAndPreservesTaskHistory(t *testing.T) {
	s := newTestStore(t)
	base := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	west := time.FixedZone("EST", -5*60*60)
	// The latest send and reply sort earlier as strings, and were imported
	// before their older counterparts. Neither insertion order nor local time
	// is sufficient to select the current request/reply.
	current := Record{BrokerID: "acme", Status: StatusSent, SentAt: base.Add(2 * time.Hour).In(west)}
	older := Record{BrokerID: "acme", Status: StatusSent, SentAt: base}
	for _, r := range []*Record{&current, &older} {
		if err := s.Add(r); err != nil {
			t.Fatal(err)
		}
	}
	received := base.Add(3 * time.Hour).In(time.FixedZone("", -5*60*60))
	for _, r := range []BrokerResponse{
		{BrokerID: "acme", ResponseType: "no_data_held", Confidence: 0.85, ReceivedAt: received},
		{BrokerID: "acme", ResponseType: "pending", NeedsReview: true, ReceivedAt: base.Add(150 * time.Minute)},
	} {
		if err := s.AddBrokerResponse(&r); err != nil {
			t.Fatal(err)
		}
	}
	var tasks []PendingTask
	for i, status := range []string{"pending", "completed", "skipped"} {
		task := PendingTask{BrokerID: "acme", TaskType: TaskReview, Notes: "Original note"}
		if err := s.AddPendingTask(&task); err != nil {
			t.Fatal(err)
		}
		created := base.Add(time.Duration(121+i) * time.Minute)
		var completed any
		if status != "pending" {
			completed = base.Add(140 * time.Minute)
		}
		if _, err := s.db.Exec(`UPDATE pending_tasks SET status=?,created_at=?,completed_at=? WHERE id=?`, status, created, completed, task.ID); err != nil {
			t.Fatal(err)
		}
		task.Status = status
		tasks = append(tasks, task)
	}
	if err := s.ResolveNoData("default", "acme", true); err != nil {
		t.Fatal(err)
	}
	latest, err := s.LatestRequests("default")
	if err != nil || len(latest) != 1 || latest[0].ID != current.ID || latest[0].PipelineStatus != PipelineNoData {
		t.Fatalf("latest=%+v err=%v", latest, err)
	}
	var oldStage string
	if err := s.db.QueryRow(`SELECT pipeline_status FROM removal_requests WHERE id=?`, older.ID).Scan(&oldStage); err != nil {
		t.Fatal(err)
	}
	if oldStage != string(PipelineEmailSent) {
		t.Fatalf("changed previous request: %s", oldStage)
	}
	for _, task := range tasks {
		got, err := s.GetPendingTaskByID(task.ID, "default")
		if err != nil {
			t.Fatal(err)
		}
		if task.Status == "pending" {
			if got.Status != "completed" || !strings.Contains(got.Notes, "no data held") {
				t.Fatalf("pending work not resolved: %+v", got)
			}
		} else if got.Status != task.Status || got.Notes != task.Notes || !got.CompletedAt.Valid || !got.CompletedAt.Time.Equal(base.Add(140*time.Minute)) {
			t.Fatalf("changed task history: %+v", got)
		}
	}
	reviews, err := s.GetBrokerResponses("default", "", true, -1)
	if err != nil || len(reviews) != 0 {
		t.Fatalf("superseded reviews=%+v err=%v", reviews, err)
	}
}

func TestNoDataReclassifyResolvesAfterMigration(t *testing.T) {
	s := newTestStore(t)
	sent := time.Now().Add(-48 * time.Hour)
	if err := s.Add(&Record{ProfileID: "default", BrokerID: "acme", BrokerName: "Acme", Status: StatusSent, SentAt: sent}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdatePipelineStatus("default", "acme", PipelineRejected); err != nil {
		t.Fatal(err)
	}
	if err := s.AddBrokerResponse(&BrokerResponse{ProfileID: "default", BrokerID: "acme", BrokerName: "Acme", ResponseType: "rejected", EmailBody: "no records", ReceivedAt: sent.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if err := s.MigrateNoData(func(BrokerResponse) (bool, float64, bool) { return true, 0.85, false }); err != nil {
		t.Fatal(err)
	}
	if err := s.ResolveNoData("default", "acme", true); err != nil {
		t.Fatal(err)
	}
	reqs, _ := s.LatestRequests("default")
	if reqs[0].PipelineStatus != PipelineNoData {
		t.Fatalf("status=%s, want %s after reclassify", reqs[0].PipelineStatus, PipelineNoData)
	}
}

func TestResolveNoDataCases(t *testing.T) {
	sent := time.Now().Add(-48 * time.Hour)
	for _, tc := range []struct {
		name         string
		stage        PipelineStatus
		replyType    string
		replyAge     time.Duration // relative to sent
		needsReview  bool
		confidence   float64
		laterTask    bool
		reclassified bool
		want         PipelineStatus
	}{
		{"confident reply resolves", PipelineRejected, "no_data_held", time.Hour, false, 0.9, false, true, PipelineNoData},
		{"not a no-data reply", PipelineRejected, "rejected", time.Hour, false, 0.9, false, true, PipelineRejected},
		{"reply before the send", PipelineRejected, "no_data_held", -time.Hour, false, 0.9, false, true, PipelineRejected},
		{"low confidence", PipelineRejected, "no_data_held", time.Hour, false, 0.5, false, true, PipelineRejected},
		{"needs review", PipelineRejected, "no_data_held", time.Hour, true, 0.9, false, true, PipelineRejected},
		{"later task blocks", PipelineRejected, "no_data_held", time.Hour, false, 0.9, true, true, PipelineRejected},
		{"form stage kept on upgrade", PipelineFormRequired, "no_data_held", time.Hour, false, 0.9, false, true, PipelineFormRequired},
		{"form stage moves when not upgrading", PipelineFormRequired, "no_data_held", time.Hour, false, 0.9, false, false, PipelineNoData},
		{"unknown stage untouched", "custom", "no_data_held", time.Hour, false, 0.9, false, true, "custom"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestStore(t)
			if err := s.Add(&Record{ProfileID: "default", BrokerID: "acme", BrokerName: "Acme", Status: StatusSent, SentAt: sent}); err != nil {
				t.Fatal(err)
			}
			if err := s.OverridePipelineStatus("default", "acme", tc.stage); err != nil {
				t.Fatal(err)
			}
			if err := s.AddBrokerResponse(&BrokerResponse{ProfileID: "default", BrokerID: "acme", BrokerName: "Acme", ResponseType: tc.replyType, EmailBody: "x", NeedsReview: tc.needsReview, Confidence: tc.confidence, ReceivedAt: sent.Add(tc.replyAge)}); err != nil {
				t.Fatal(err)
			}
			if tc.laterTask {
				task := PendingTask{ProfileID: "default", BrokerID: "acme", BrokerName: "Acme", TaskType: TaskReview, Notes: "later"}
				if err := s.AddPendingTask(&task); err != nil {
					t.Fatal(err)
				}
				if _, err := s.db.Exec(`UPDATE pending_tasks SET created_at = ? WHERE id = ?`, sent.Add(3*time.Hour), task.ID); err != nil {
					t.Fatal(err)
				}
			}
			if err := s.ResolveNoData("default", "acme", tc.reclassified); err != nil {
				t.Fatal(err)
			}
			reqs, _ := s.LatestRequests("default")
			if reqs[0].PipelineStatus != tc.want {
				t.Fatalf("status=%s, want %s", reqs[0].PipelineStatus, tc.want)
			}
		})
	}
}

func TestResolveNoDataNoRepliesIsNoop(t *testing.T) {
	s := newTestStore(t)
	if err := s.ResolveNoData("default", "nobody", true); err != nil {
		t.Fatal(err)
	}
}
