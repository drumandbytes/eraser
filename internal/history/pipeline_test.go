package history

import (
	"testing"
	"time"
)

func TestActionItemsCounters(t *testing.T) {
	a := ActionItems{
		Forms:   []FormWithStatus{{}, {}},
		Tasks:   []PendingTask{{TaskType: TaskManualForm}, {TaskType: TaskReview}},
		Reviews: []BrokerResponse{{}},
	}
	if a.Total() != 5 || a.NeedsReview() != 2 || a.FormsToFill() != 3 {
		t.Fatalf("total=%d review=%d forms=%d", a.Total(), a.NeedsReview(), a.FormsToFill())
	}
}

func TestGetActionItemsDedupesFormTasks(t *testing.T) {
	s := newTestStore(t)
	if err := s.AddPendingTask(&PendingTask{ProfileID: "default", BrokerID: "acme", BrokerName: "Acme", TaskType: TaskManualForm}); err != nil {
		t.Fatal(err)
	}
	if err := s.AddBrokerResponse(&BrokerResponse{ProfileID: "default", BrokerID: "beta", BrokerName: "Beta", ResponseType: "unknown", NeedsReview: true, ReceivedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	a, err := s.GetActionItems("default")
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Tasks) != 1 || len(a.Reviews) != 1 || a.FormsToFill() != 1 || a.NeedsReview() != 1 {
		t.Fatalf("items=%+v", a)
	}
}

func TestQueryBrokerResponsesFiltersSearchesAndPages(t *testing.T) {
	s := newTestStore(t)
	base := time.Now().Add(-time.Hour)
	for i, r := range []BrokerResponse{
		{BrokerID: "acme", BrokerName: "Acme", ResponseType: "no_data_held", EmailBody: "Nothing on file", NeedsReview: false},
		{BrokerID: "bücher", BrokerName: "Büchermann", ResponseType: "rejected", EmailBody: "We 100% decline", NeedsReview: true},
		{BrokerID: "gamma", BrokerName: "Gamma", ResponseType: "rejected", EmailBody: "Your file_name is wrong", NeedsReview: false},
	} {
		r.ProfileID = "default"
		r.EmailSubject = "Reply"
		r.ReceivedAt = base.Add(time.Duration(i) * time.Minute)
		if err := s.AddBrokerResponse(&r); err != nil {
			t.Fatal(err)
		}
	}
	check := func(name string, q ResponseQuery, wantTotal int, wantIDs ...string) {
		t.Helper()
		rows, total, err := s.QueryBrokerResponses("default", q)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if total != wantTotal || len(rows) != len(wantIDs) {
			t.Fatalf("%s: total=%d rows=%d, want %d/%d", name, total, len(rows), wantTotal, len(wantIDs))
		}
	}
	check("all", ResponseQuery{Limit: 10}, 3, "a", "b", "c")
	check("type", ResponseQuery{Type: "rejected", Limit: 10}, 2, "b", "c")
	check("review", ResponseQuery{NeedsReview: true, Limit: 10}, 1, "b")
	check("page", ResponseQuery{Limit: 2, Offset: 2}, 3, "c")
	check("unicode case", ResponseQuery{Search: "BÜCHER", Limit: 10}, 1, "b")
	check("percent is literal", ResponseQuery{Search: "100%", Limit: 10}, 1, "b")
	check("underscore is literal", ResponseQuery{Search: "file_name", Limit: 10}, 1, "c")
	check("no match", ResponseQuery{Search: "%_", Limit: 10}, 0)
	ids, err := s.MatchingResponseIDs("default", "nothing on")
	if err != nil || len(ids) != 1 {
		t.Fatalf("matching ids=%v err=%v", ids, err)
	}
}

func TestLatestRequestsKeepsNewestPerBroker(t *testing.T) {
	s := newTestStore(t)
	old := time.Now().Add(-72 * time.Hour)
	if err := s.Add(&Record{ProfileID: "default", BrokerID: "acme", BrokerName: "Acme", Status: StatusSent, SentAt: old}); err != nil {
		t.Fatal(err)
	}
	if err := s.Add(&Record{ProfileID: "default", BrokerID: "acme", BrokerName: "Acme", Status: StatusSent, SentAt: old.Add(24 * time.Hour)}); err != nil {
		t.Fatal(err)
	}
	latest, err := s.LatestRequests("default")
	if err != nil {
		t.Fatal(err)
	}
	if len(latest) != 1 || !latest[0].SentAt.Equal(old.Add(24*time.Hour)) || latest[0].PipelineStatus != PipelineEmailSent {
		t.Fatalf("latest=%+v", latest)
	}
}
