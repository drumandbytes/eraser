package history

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestGetAllRequestsOldestFirstWithPipeline(t *testing.T) {
	s := newTestStore(t)
	now := time.Now()
	addRecordForProfile(t, s, "a", "late", StatusSent, now)
	addRecordForProfile(t, s, "a", "early", StatusFailed, now.Add(-time.Hour))
	addRecordForProfile(t, s, "b", "other", StatusSent, now)
	if err := s.UpdatePipelineStatus("a", "late", PipelineFormRequired); err != nil {
		t.Fatal(err)
	}

	recs, err := s.GetAllRequests("a")
	if err != nil {
		t.Fatalf("GetAllRequests: %v", err)
	}
	if len(recs) != 2 || recs[0].BrokerID != "early" || recs[1].BrokerID != "late" {
		t.Fatalf("got %+v, want early then late, profile b excluded", recs)
	}
	if recs[1].PipelineStatus != PipelineFormRequired || recs[0].PipelineStatus != PipelineEmailSent {
		t.Errorf("pipeline statuses = %q, %q", recs[0].PipelineStatus, recs[1].PipelineStatus)
	}
	if recs[0].SentMethod != "smtp" {
		t.Errorf("sent method default = %q, want smtp", recs[0].SentMethod)
	}
}

func TestGetMonthlyStats(t *testing.T) {
	s := newTestStore(t)
	if sent, failed, err := s.GetMonthlyStats(""); err != nil || sent != 0 || failed != 0 {
		t.Fatalf("empty store: %d, %d, %v", sent, failed, err)
	}
	now := time.Now()
	addRecord(t, s, "a", StatusSent, now)
	addRecord(t, s, "b", StatusFailed, now)
	addRecord(t, s, "c", StatusSent, now.AddDate(0, -2, 0)) // before this month
	sent, failed, err := s.GetMonthlyStats("")
	if err != nil || sent != 1 || failed != 1 {
		t.Errorf("GetMonthlyStats = %d, %d, %v; want 1, 1", sent, failed, err)
	}
}

func TestGetBrokerStatus(t *testing.T) {
	s := newTestStore(t)
	now := time.Now().Truncate(time.Second)

	bs, err := s.GetBrokerStatus("", "never")
	if err != nil || bs.TotalSent != 0 || !bs.LastSent.IsZero() || bs.Status != "" {
		t.Fatalf("unknown broker = %+v, %v", bs, err)
	}

	addRecord(t, s, "acme", StatusSent, now.Add(-time.Hour))
	addRecord(t, s, "acme", StatusFailed, now)
	bs, err = s.GetBrokerStatus("", "acme")
	if err != nil {
		t.Fatal(err)
	}
	if bs.TotalSent != 2 || bs.Status != StatusFailed || !bs.LastSent.Equal(now) {
		t.Errorf("acme = %+v, want 2 records, latest failed at %v", bs, now)
	}
}

func TestDeleteByStatusAndAllHistory(t *testing.T) {
	s := newTestStore(t)
	now := time.Now()
	addRecordForProfile(t, s, "a", "x", StatusSent, now)
	addRecordForProfile(t, s, "a", "y", StatusFailed, now)
	addRecordForProfile(t, s, "a", "z", StatusFailed, now)
	addRecordForProfile(t, s, "b", "x", StatusFailed, now)

	n, err := s.DeleteByStatus("a", StatusFailed)
	if err != nil || n != 2 {
		t.Fatalf("DeleteByStatus = %d, %v; want 2", n, err)
	}
	if total, _, _, _ := s.GetStats("b"); total != 1 {
		t.Error("DeleteByStatus touched another profile")
	}

	n, err = s.DeleteAllHistory("a")
	if err != nil || n != 1 {
		t.Fatalf("DeleteAllHistory = %d, %v; want 1", n, err)
	}
	if total, _, _, _ := s.GetStats("b"); total != 1 {
		t.Error("DeleteAllHistory touched another profile")
	}
}

func TestDBPaths(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if got, want := DefaultDBPath(), filepath.Join(home, ".eraser", "history.db"); got != want {
		t.Errorf("DefaultDBPath = %q, want %q", got, want)
	}
	if got := DBPathFor(""); got != DefaultDBPath() {
		t.Errorf("DBPathFor(\"\") = %q", got)
	}
	if got := DBPathFor("/x/alt/config.yaml"); got != filepath.Join("/x/alt", "history.db") {
		t.Errorf("DBPathFor = %q", got)
	}
}

func TestNewStoreTightensPermissionsAndReopens(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "dir", "history.db")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := NewStore(path)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	addRecord(t, s, "acme", StatusSent, time.Now())
	_ = s.Close()

	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("db mode = %v, %v; want 0600", info.Mode().Perm(), err)
	}

	// Reopening runs the migrations again over existing tables.
	s, err = NewStore(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = s.Close() }()
	if total, _, _, _ := s.GetStats(""); total != 1 {
		t.Errorf("record lost across reopen: total = %d", total)
	}
}

func TestNewStoreErrors(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "file")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewStore(filepath.Join(blocker, "history.db")); err == nil {
		t.Error("expected an error when the parent path is a file")
	}
	if _, err := NewStore(dir); err == nil {
		t.Error("expected an error when the db path is a directory")
	}
	garbage := filepath.Join(dir, "garbage.db")
	if err := os.WriteFile(garbage, []byte("this is not an sqlite database, just some text padding it out a bit"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewStore(garbage); err == nil {
		t.Error("expected a migration error on a non-SQLite file")
	}
}

func TestAddColumnIfMissingReturnsRealErrors(t *testing.T) {
	s := newTestStore(t)
	if err := addColumnIfMissing(s.db, `ALTER TABLE removal_requests ADD COLUMN pipeline_status TEXT`); err != nil {
		t.Errorf("duplicate column should be ignored: %v", err)
	}
	if err := addColumnIfMissing(s.db, `ALTER TABLE nope ADD COLUMN x TEXT`); err != nil {
		t.Errorf("missing table should be ignored: %v", err)
	}
	if err := addColumnIfMissing(s.db, `ALTER TABLE removal_requests ADD COLUMN`); err == nil {
		t.Error("a syntax error must be returned")
	}
}

func TestParseTimeHelpers(t *testing.T) {
	want := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	for _, in := range []string{
		"2026-03-04 05:06:07 +0000 UTC m=+0.000000001",
		"2026-03-04T05:06:07Z",
		"2026-03-04 05:06:07+00:00",
		"2026-03-04 05:06:07",
	} {
		got, err := parseSQLiteTime(in)
		if err != nil || !got.Equal(want) {
			t.Errorf("parseSQLiteTime(%q) = %v, %v", in, got, err)
		}
	}
	if _, err := parseSQLiteTime("yesterday"); err == nil {
		t.Error("parseSQLiteTime accepted garbage")
	}

	if got := parseFlexibleTimeString(sql.NullString{}); !got.IsZero() {
		t.Errorf("NULL = %v, want zero", got)
	}
	for _, in := range []string{"2026-03-04T05:06:07Z", "2026-03-04 05:06:07"} {
		if got := parseFlexibleTimeString(sql.NullString{String: in, Valid: true}); !got.Equal(want) {
			t.Errorf("parseFlexibleTimeString(%q) = %v", in, got)
		}
	}
}

// Rows written by old versions carry text timestamps; a broken one is
// skipped in the status map and surfaces as an error for the cooldown.
func TestBadSentAtText(t *testing.T) {
	s := newTestStore(t)
	addRecord(t, s, "good", StatusSent, time.Now())
	if _, err := s.db.Exec(`INSERT INTO removal_requests (profile_id, broker_id, broker_name, email, template, status, sent_at)
		VALUES ('default', 'bad', 'bad', 'b@x', 'gdpr', 'sent', 'not a time')`); err != nil {
		t.Fatal(err)
	}

	statuses, err := s.GetAllBrokerStatuses("")
	if err != nil {
		t.Fatalf("GetAllBrokerStatuses: %v", err)
	}
	if statuses["good"].LastSent.IsZero() || !statuses["bad"].LastSent.IsZero() {
		t.Errorf("statuses = %+v", statuses)
	}
	if bs, err := s.GetBrokerStatus("", "bad"); err != nil || !bs.LastSent.IsZero() || bs.TotalSent != 1 {
		t.Errorf("GetBrokerStatus(bad) = %+v, %v", bs, err)
	}
	if _, err := s.LastSuccessfulSendTimes(""); err == nil {
		t.Error("LastSuccessfulSendTimes should report the unparseable row")
	}
}

func TestBrokerResponseQueries(t *testing.T) {
	s := newTestStore(t)
	t0 := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	add := func(profile, broker, typ, subject string, review bool, received time.Time, form string) {
		t.Helper()
		if err := s.AddBrokerResponse(&BrokerResponse{
			ProfileID: profile, BrokerID: broker, BrokerName: broker, ResponseType: typ,
			EmailSubject: subject, EmailBody: "body of " + subject, NeedsReview: review,
			ReceivedAt: received, FormURL: form,
		}); err != nil {
			t.Fatal(err)
		}
	}
	add("a", "acme", "success", "Done", false, t0.Add(2*time.Hour), "")
	add("a", "globex", "form_required", "Use form", true, t0, "https://globex.example/f")
	add("a", "initech", "form_required", "Form too", false, t0.Add(time.Hour), "https://initech.example/f")
	add("b", "acme", "success", "Done", false, t0, "")

	exp, err := s.GetBrokerResponsesForExport("a")
	if err != nil || len(exp) != 3 || exp[0].BrokerID != "globex" || exp[2].BrokerID != "acme" {
		t.Fatalf("export = %+v, %v; want oldest first, profile a only", exp, err)
	}
	if exp[0].EmailBody == "" {
		t.Error("export must include bodies")
	}

	list, err := s.GetBrokerResponses("a", "form_required", false, 10)
	if err != nil || len(list) != 2 {
		t.Fatalf("filtered by type = %d, %v", len(list), err)
	}
	if list[0].EmailBody != "" {
		t.Error("list queries must not load bodies")
	}
	if list, _ := s.GetBrokerResponses("a", "", true, 10); len(list) != 1 || list[0].BrokerID != "globex" {
		t.Errorf("needs-review filter = %+v", list)
	}
	if list, _ := s.GetBrokerResponses("a", "", false, 1); len(list) != 1 {
		t.Errorf("limit not applied: %d", len(list))
	}

	stats, err := s.GetResponseStats("a")
	if err != nil || stats["form_required"] != 2 || stats["success"] != 1 {
		t.Errorf("stats = %v, %v", stats, err)
	}

	found, err := s.FindBrokerResponseBySubject("a", "acme", "Done")
	if err != nil || found == nil || found.ProfileID != "a" {
		t.Errorf("FindBrokerResponseBySubject = %+v, %v", found, err)
	}
	if found, err := s.FindBrokerResponseBySubject("a", "acme", "nope"); err != nil || found != nil {
		t.Errorf("missing subject = %+v, %v; want nil, nil", found, err)
	}

	if err := s.ClearBrokerResponses(); err != nil {
		t.Fatal(err)
	}
	if all, _ := s.GetAllBrokerResponses(); len(all) != 0 {
		t.Errorf("ClearBrokerResponses left %d rows", len(all))
	}
}

func TestGetBrokerResponseByIDMissing(t *testing.T) {
	s := newTestStore(t)
	if r, err := s.GetBrokerResponseByID(999, ""); err != nil || r != nil {
		t.Errorf("missing id = %+v, %v; want nil, nil", r, err)
	}
}

func TestMarkBrokerResponseReviewedMissing(t *testing.T) {
	s := newTestStore(t)
	if err := s.MarkBrokerResponseReviewed(999, ""); err != sql.ErrNoRows {
		t.Errorf("err = %v, want sql.ErrNoRows", err)
	}
}

func TestFormsWithStatus(t *testing.T) {
	s := newTestStore(t)
	now := time.Now()
	form := func(broker string) {
		t.Helper()
		if err := s.AddBrokerResponse(&BrokerResponse{ProfileID: "a", BrokerID: broker, BrokerName: broker,
			ResponseType: "form_required", FormURL: "https://" + broker + ".example/form", ReceivedAt: now}); err != nil {
			t.Fatal(err)
		}
	}
	sent := func(broker string, p PipelineStatus) {
		t.Helper()
		addRecordForProfile(t, s, "a", broker, StatusSent, now)
		if p != "" {
			if err := s.UpdatePipelineStatus("a", broker, p); err != nil {
				t.Fatal(err)
			}
		}
	}
	task := func(broker, status string) {
		t.Helper()
		pt := addPendingTaskForProfile(t, s, "a", broker)
		if status != "pending" {
			if err := s.CompletePendingTask(pt.ID, "a", status); err != nil {
				t.Fatal(err)
			}
		}
	}

	form("done-task")
	task("done-task", "completed")
	form("skip-task")
	task("skip-task", "skipped")
	form("captcha")
	task("captcha", "pending")
	form("filled")
	sent("filled", PipelineFormFilled)
	form("confirmed")
	sent("confirmed", PipelineConfirmed)
	form("failed")
	sent("failed", PipelineFailed)
	form("rejected")
	sent("rejected", PipelineRejected)
	form("pending")
	form("pending") // a second reply for the same broker is deduped
	// No form URL: not a form at all.
	if err := s.AddBrokerResponse(&BrokerResponse{ProfileID: "a", BrokerID: "noform", BrokerName: "noform", ResponseType: "success"}); err != nil {
		t.Fatal(err)
	}

	forms, err := s.GetFormsWithStatus("a")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, f := range forms {
		if _, dup := got[f.BrokerID]; dup {
			t.Errorf("broker %q listed twice", f.BrokerID)
		}
		got[f.BrokerID] = f.Status
	}
	want := map[string]string{
		"done-task": "filled", "skip-task": "skipped", "captcha": "captcha",
		"filled": "filled", "confirmed": "filled", "failed": "failed",
		"rejected": "skipped", "pending": "pending",
	}
	if len(got) != len(want) {
		t.Errorf("forms = %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s: status %q, want %q", k, got[k], v)
		}
	}

	pending, filled, captcha, failed, skipped, err := s.GetFormStats("a")
	if err != nil || pending != 1 || filled != 3 || captcha != 1 || failed != 1 || skipped != 2 {
		t.Errorf("GetFormStats = %d %d %d %d %d, %v", pending, filled, captcha, failed, skipped, err)
	}
	if forms, _ := s.GetFormsWithStatus("b"); len(forms) != 0 {
		t.Errorf("profile b sees %d forms", len(forms))
	}
}

func TestPendingTaskQueriesAndStats(t *testing.T) {
	s := newTestStore(t)
	captcha := addPendingTaskForProfile(t, s, "a", "acme")
	manual := &PendingTask{ProfileID: "a", BrokerID: "globex", BrokerName: "globex", TaskType: TaskManualForm,
		BrowserState: `{"x":1}`, ScreenshotPath: "/tmp/s.png"}
	if err := s.AddPendingTask(manual); err != nil {
		t.Fatal(err)
	}
	addPendingTaskForProfile(t, s, "b", "acme")
	if err := s.CompletePendingTask(captcha.ID, "a", "completed"); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		typ    TaskType
		status string
		want   int
	}{
		{"", "", 2},
		{TaskCaptcha, "", 1},
		{"", "pending", 1},
		{TaskManualForm, "pending", 1},
		{TaskCaptcha, "pending", 0},
	}
	for _, c := range cases {
		tasks, err := s.GetPendingTasks("a", c.typ, c.status)
		if err != nil || len(tasks) != c.want {
			t.Errorf("GetPendingTasks(%q, %q) = %d, %v; want %d", c.typ, c.status, len(tasks), err, c.want)
		}
	}
	tasks, _ := s.GetPendingTasks("a", TaskManualForm, "")
	if len(tasks) != 1 || tasks[0].BrowserState != `{"x":1}` || tasks[0].ScreenshotPath != "/tmp/s.png" || tasks[0].CreatedAt.IsZero() {
		t.Errorf("manual task round-trip = %+v", tasks)
	}

	pending, completed, skipped, err := s.GetPendingTaskStats("a")
	if err != nil || pending != 1 || completed != 1 || skipped != 0 {
		t.Errorf("stats = %d %d %d, %v", pending, completed, skipped, err)
	}
	if p, c, sk, err := s.GetPendingTaskStats("nobody"); err != nil || p+c+sk != 0 {
		t.Errorf("empty profile stats = %d %d %d, %v", p, c, sk, err)
	}
}

// UpdatePipelineStatus moves only the broker's latest record; the stats
// count each broker once, by its latest record.
func TestPipelineStatusAndStats(t *testing.T) {
	s := newTestStore(t)
	now := time.Now()
	addRecordForProfile(t, s, "a", "acme", StatusSent, now.Add(-time.Hour))
	addRecordForProfile(t, s, "a", "acme", StatusSent, now)
	addRecordForProfile(t, s, "a", "globex", StatusSent, now)
	addRecordForProfile(t, s, "b", "acme", StatusSent, now)
	if err := s.UpdatePipelineStatus("a", "acme", PipelineConfirmed); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`UPDATE removal_requests SET pipeline_status = NULL WHERE broker_id = 'globex'`); err != nil {
		t.Fatal(err)
	}

	recs, _ := s.GetAllRequests("a")
	if recs[0].PipelineStatus != PipelineEmailSent || recs[1].PipelineStatus != PipelineConfirmed {
		t.Errorf("only the latest acme record should move: %q, %q", recs[0].PipelineStatus, recs[1].PipelineStatus)
	}
	stats, err := s.GetPipelineStats("a")
	if err != nil {
		t.Fatal(err)
	}
	// globex's NULL pipeline status counts as email_sent (pre-pipeline rows).
	if stats[PipelineConfirmed] != 1 || stats[PipelineEmailSent] != 1 || len(stats) != 2 {
		t.Errorf("pipeline stats = %v", stats)
	}
	if b, _ := s.GetAllRequests("b"); b[0].PipelineStatus != PipelineEmailSent {
		t.Error("UpdatePipelineStatus leaked into profile b")
	}
}

// Every query wraps its error rather than panicking or returning zero
// values as success when the database is gone.
func TestClosedStoreReturnsErrors(t *testing.T) {
	s := newTestStore(t)
	_ = s.Close()
	now := time.Now()

	errs := map[string]error{}
	errs["Add"] = s.Add(&Record{})
	_, errs["GetRecentRequests"] = s.GetRecentRequests("", 10)
	_, errs["GetAllRequests"] = s.GetAllRequests("")
	_, _, _, errs["GetStats"] = s.GetStats("")
	_, _, errs["GetMonthlyStats"] = s.GetMonthlyStats("")
	_, errs["CountSentSince"] = s.CountSentSince("", now)
	_, errs["LastSuccessfulSendTimes"] = s.LastSuccessfulSendTimes("")
	_, errs["MarkFailed"] = s.MarkFailed("", "x", "n")
	_, errs["ResolveProfileForBroker"] = s.ResolveProfileForBroker("x")
	_, errs["GetAllBrokerStatuses"] = s.GetAllBrokerStatuses("")
	_, errs["GetBrokerStatus"] = s.GetBrokerStatus("", "x")
	_, errs["DeleteByStatus"] = s.DeleteByStatus("", StatusSent)
	_, errs["DeleteAllHistory"] = s.DeleteAllHistory("")
	errs["AddBrokerResponse"] = s.AddBrokerResponse(&BrokerResponse{})
	_, errs["AddBrokerResponseIfNew"] = s.AddBrokerResponseIfNew(&BrokerResponse{})
	_, errs["GetBrokerResponseByID"] = s.GetBrokerResponseByID(1, "")
	_, errs["FindBrokerResponseBySubject"] = s.FindBrokerResponseBySubject("", "x", "s")
	errs["UpdateBrokerResponseClassification"] = s.UpdateBrokerResponseClassification(1, "", "success", "", "", 1, false)
	errs["UpdateBrokerResponseBody"] = s.UpdateBrokerResponseBody(1, "", "b")
	errs["MarkBrokerResponseReviewed"] = s.MarkBrokerResponseReviewed(1, "")
	errs["ClearBrokerResponses"] = s.ClearBrokerResponses()
	_, errs["GetAllBrokerResponses"] = s.GetAllBrokerResponses()
	_, errs["GetResponseStats"] = s.GetResponseStats("")
	_, errs["GetFormsWithStatus"] = s.GetFormsWithStatus("")
	_, _, _, _, _, errs["GetFormStats"] = s.GetFormStats("")
	errs["AddPendingTask"] = s.AddPendingTask(&PendingTask{})
	_, errs["GetPendingTasks"] = s.GetPendingTasks("", "", "")
	_, errs["GetPendingTaskByID"] = s.GetPendingTaskByID(1, "")
	errs["CompletePendingTask"] = s.CompletePendingTask(1, "", "completed")
	errs["MarkTaskOpened"] = s.MarkTaskOpened(1, "")
	_, _, _, errs["GetPendingTaskStats"] = s.GetPendingTaskStats("")
	errs["UpdatePipelineStatus"] = s.UpdatePipelineStatus("", "x", PipelineConfirmed)
	_, errs["GetPipelineStats"] = s.GetPipelineStats("")

	for name, err := range errs {
		if err == nil {
			t.Errorf("%s on a closed store returned nil error", name)
		}
	}
}
