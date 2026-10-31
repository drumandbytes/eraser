package reclassify

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/drumandbytes/eraser/internal/broker"
	"github.com/drumandbytes/eraser/internal/config"
	"github.com/drumandbytes/eraser/internal/history"
	"github.com/drumandbytes/eraser/internal/imaptest"
	_ "modernc.org/sqlite"
)

func TestRunWithoutStoreOrResponses(t *testing.T) {
	if _, err := Run(context.Background(), nil, nil, nil); err == nil {
		t.Error("a missing store must be an error")
	}
	store, err := history.NewStore(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	sum, err := Run(context.Background(), store, nil, nil)
	if err != nil || sum.Total != 0 {
		t.Errorf("empty history: %+v, %v", sum, err)
	}
}

func seed(t *testing.T, store *history.Store, id, brokerID, respType, subject, body string) {
	t.Helper()
	if err := store.AddBrokerResponse(&history.BrokerResponse{
		ProfileID: "default", BrokerID: brokerID, BrokerName: brokerID, ResponseType: respType,
		EmailFrom: "privacy@" + brokerID + ".example", EmailSubject: subject, EmailBody: body, ReceivedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
}

func TestRunReclassifiesWithAndWithoutBodies(t *testing.T) {
	store, err := history.NewStore(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	// A deletion confirmation saved as "unknown" is fixed from its body...
	seed(t, store, "1", "acme", "unknown", "Your request", "We have deleted your data and removed all records as requested.")
	// ...and a body-less one falls back to its subject.
	seed(t, store, "2", "globex", "unknown", "Please use our web form to submit your request", "")

	sum, err := Run(context.Background(), store, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if sum.Total != 2 || sum.Updated < 1 || sum.Updated+sum.Unchanged != 2 {
		t.Errorf("summary = %+v", sum)
	}
	if sum.Success+sum.FormRequired+sum.ConfirmRequired+sum.Pending+sum.Rejected+sum.NoData+sum.Unknown != 2 {
		t.Errorf("every reply must be counted once: %+v", sum)
	}
	again, err := Run(context.Background(), store, nil, nil)
	if err != nil || again.Updated != 0 {
		t.Errorf("a second run must change nothing: %+v, %v", again, err)
	}
}

func TestRunBackfillsMissingBodiesFromTheInbox(t *testing.T) {
	store, err := history.NewStore(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	seed(t, store, "1", "acme", "unknown", "Re: your request", "")

	srv := imaptest.Start(t, false)
	srv.Deliver(t, "INBOX", "privacy@acme.example", "Re: your request", "We have deleted your data and removed all records as requested.")
	brokers := []broker.Broker{{ID: "acme", Name: "Acme", Email: "privacy@acme.example"}}

	sum, err := Run(context.Background(), store, []config.InboxConfig{srv.Inbox}, brokers)
	if err != nil {
		t.Fatal(err)
	}
	if sum.BodiesUpdated != 1 {
		t.Errorf("BodiesUpdated = %d, want 1 (%+v)", sum.BodiesUpdated, sum)
	}
	all, _ := store.GetAllBrokerResponses()
	if len(all) != 1 || all[0].EmailBody == "" {
		t.Errorf("body not stored: %+v", all)
	}

	// An unreachable inbox is skipped, not fatal.
	down := []config.InboxConfig{{Enabled: true, Server: "127.0.0.1", Port: 1, Email: "a", Password: "p"}}
	seed(t, store, "2", "acme", "unknown", "Another", "")
	if _, err := Run(context.Background(), store, down, brokers); err != nil {
		t.Errorf("unreachable inbox must not fail the run: %v", err)
	}

	_ = store.Close()
	if _, err := Run(context.Background(), store, nil, nil); err == nil {
		t.Error("a closed store must be an error")
	}
}

func TestRunCountsEveryKindOfReply(t *testing.T) {
	store, err := history.NewStore(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	bodies := []string{
		"We have deleted your data and removed all records as requested.",
		"Please use our web form to submit your request: https://acme.example/optout",
		"Please confirm your request by clicking this link: https://acme.example/confirm?token=abc123",
		"We have no record of you in our systems and were unable to locate any data about you.",
		"We are not a data broker and do not hold consumer data. This request is not in scope.",
		"Thanks, we received your request and will respond within 30 days.",
		"hello there",
	}
	for i, body := range bodies {
		seed(t, store, "", "broker"+string(rune('a'+i)), "unknown", "Re: your request", body)
	}
	sum, err := Run(context.Background(), store, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	counted := sum.Success + sum.FormRequired + sum.ConfirmRequired + sum.Pending + sum.Rejected + sum.NoData + sum.Unknown
	if sum.Total != len(bodies) || counted != len(bodies) {
		t.Fatalf("summary = %+v", sum)
	}
	kinds := 0
	for _, n := range []int{sum.Success, sum.FormRequired, sum.ConfirmRequired, sum.Pending, sum.Rejected, sum.NoData, sum.Unknown} {
		if n > 0 {
			kinds++
		}
	}
	if kinds < 4 {
		t.Errorf("expected the replies to land in several categories, got %+v", sum)
	}
}

// A write failure stops the run with a clear error instead of reporting success.
func TestRunStopsWhenAnUpdateFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.db")
	store, err := history.NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	seed(t, store, "1", "acme", "unknown", "Re: request", "We have deleted your data and removed all records as requested.")

	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.Exec(`CREATE TRIGGER block_updates BEFORE UPDATE ON broker_responses BEGIN SELECT RAISE(ABORT, 'read-only for the test'); END`); err != nil {
		t.Fatal(err)
	}

	if _, err := Run(context.Background(), store, nil, nil); err == nil || !strings.Contains(err.Error(), "failed to update response") {
		t.Errorf("err = %v", err)
	}
}
