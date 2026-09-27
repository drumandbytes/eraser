package inbox

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/drumandbytes/eraser/internal/history"
)

func TestRecordReply(t *testing.T) {
	store, err := history.NewStore(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	if err := store.Add(&history.Record{ProfileID: "jane", BrokerID: "acme", BrokerName: "Acme", Email: "privacy@acme.example", Template: "gdpr", Status: history.StatusSent, SentAt: time.Now()}); err != nil {
		t.Fatal(err)
	}

	reply := &Email{
		From: "privacy@acme.example", BrokerID: "acme", BrokerName: "Acme",
		Subject:    "Re: Erasure request",
		Body:       "We have deleted your personal data from our systems.",
		ReceivedAt: time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC),
	}

	_, outcome, err := RecordReply(store, reply, false)
	if err != nil || outcome != ReplyNew {
		t.Fatalf("first record: outcome %v, err %v; want ReplyNew", outcome, err)
	}
	reqs, err := store.GetAllRequests("jane")
	if err != nil || len(reqs) != 1 || reqs[0].PipelineStatus != history.PipelineConfirmed {
		t.Fatalf("pipeline status not advanced by a new reply: %+v (%v)", reqs, err)
	}

	if _, outcome, _ := RecordReply(store, reply, false); outcome != ReplySeen {
		t.Fatalf("second scan: outcome %v, want ReplySeen", outcome)
	}
	if _, outcome, _ := RecordReply(store, reply, true); outcome != ReplyUpdated {
		t.Fatalf("reclassify: outcome %v, want ReplyUpdated", outcome)
	}
	all, _ := store.GetAllBrokerResponses()
	if len(all) != 1 || all[0].ProfileID != "jane" || all[0].EmailBody == "" {
		t.Fatalf("want one stored reply attributed to jane with its body, got %+v", all)
	}
}
