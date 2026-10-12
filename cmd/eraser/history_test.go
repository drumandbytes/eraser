package main

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/drumandbytes/eraser/internal/history"
)

func TestOpenHistoryStoreUpgradesSavedNoDataReply(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.db")
	old, err := history.NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	r := history.BrokerResponse{BrokerID: "acme", ResponseType: "rejected", EmailSubject: "Reply", EmailBody: "We do not have any records of you.", ReceivedAt: time.Now()}
	if err := old.AddBrokerResponse(&r); err != nil {
		t.Fatal(err)
	}
	_ = old.Close()
	upgraded, err := openHistoryStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer upgraded.Close()
	got, err := upgraded.GetBrokerResponseByID(r.ID, "default")
	if err != nil || got.ResponseType != "no_data" || got.EmailBody != r.EmailBody {
		t.Fatalf("upgrade=%+v err=%v", got, err)
	}
}
