package inbox

import (
	"testing"
	"time"

	"github.com/drumandbytes/eraser/internal/history"
)

func TestMigrateNoDataResponsesRelabelsLocalReplies(t *testing.T) {
	s, err := history.NewStore(t.TempDir() + "/history.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	clear := history.BrokerResponse{ProfileID: "default", BrokerID: "acme", BrokerName: "Acme", ResponseType: "rejected", EmailSubject: "Re: request", EmailBody: "We do not have any records of you in our database.", ReceivedAt: time.Now()}
	if err := s.AddBrokerResponse(&clear); err != nil {
		t.Fatal(err)
	}
	unrelated := history.BrokerResponse{ProfileID: "default", BrokerID: "beta", BrokerName: "Beta", ResponseType: "rejected", EmailSubject: "Request denied", EmailBody: "We cannot process your request.", ReceivedAt: time.Now()}
	if err := s.AddBrokerResponse(&unrelated); err != nil {
		t.Fatal(err)
	}
	if err := MigrateNoDataResponses(s); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetBrokerResponseByID(clear.ID, "default")
	if err != nil || got.ResponseType != "no_data_held" {
		t.Fatalf("clear reply=%+v err=%v", got, err)
	}
	got, err = s.GetBrokerResponseByID(unrelated.ID, "default")
	if err != nil || got.ResponseType != "rejected" {
		t.Fatalf("unrelated reply=%+v err=%v", got, err)
	}
}
