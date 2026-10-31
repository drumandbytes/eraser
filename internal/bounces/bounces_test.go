package bounces

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/drumandbytes/eraser/internal/broker"
	"github.com/drumandbytes/eraser/internal/config"
	"github.com/drumandbytes/eraser/internal/history"
	"github.com/drumandbytes/eraser/internal/imaptest"
	"github.com/drumandbytes/eraser/internal/testenv"
)

func testBrokers() *broker.BrokerDatabase {
	return &broker.BrokerDatabase{Brokers: []broker.Broker{
		{ID: "acme", Name: "Acme", Email: "privacy@acme.example", Region: "us"},
		{ID: "globex", Name: "Globex", Email: "dpo@globex.example", Region: "eu"},
	}}
}

func deliverNDR(t *testing.T, srv *imaptest.Server, addr string) {
	t.Helper()
	srv.Deliver(t, "INBOX", "Mail Delivery Subsystem <mailer-daemon@mx.example>", "Delivery Status Notification (Failure)",
		"Delivery to the following recipient failed permanently: "+addr)
}

func TestScanSortsBounces(t *testing.T) {
	srv := imaptest.Start(t, false)
	deliverNDR(t, srv, "privacy@acme.example")
	deliverNDR(t, srv, "privacy@acme.example") // the same address twice
	deliverNDR(t, srv, "dpo@globex.example")   // never emailed
	deliverNDR(t, srv, "nobody@unknown.example")
	srv.Deliver(t, "INBOX", "postmaster@mx.example", "Undeliverable", "sorry")

	rep, err := Scan(context.Background(), []config.InboxConfig{srv.Inbox}, testBrokers(), map[string]bool{"acme": true}, 30)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Total != 5 {
		t.Fatalf("total = %d, want 5", rep.Total)
	}
	counts := map[Kind]int{}
	for _, it := range rep.Items {
		counts[it.Kind]++
	}
	if counts[Dead] != 2 || counts[NeverSent] != 1 || counts[NotInList] != 1 || counts[NoRecipient] != 1 {
		t.Errorf("kinds = %v", counts)
	}
	if dead := rep.Dead(); len(dead) != 1 || dead[0].Email != "privacy@acme.example" {
		t.Errorf("Dead() = %+v (want the duplicate collapsed)", dead)
	}
}

func TestScanFailsWhenInboxUnreachable(t *testing.T) {
	_, err := Scan(context.Background(), []config.InboxConfig{{Enabled: true, Server: "127.0.0.1", Port: 1, Email: "a", Password: "p"}}, testBrokers(), nil, 30)
	if err == nil || !strings.Contains(err.Error(), "failed to connect") {
		t.Errorf("err = %v", err)
	}
}

func TestClearEditsTheMaintainedListAndKeepsABackup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "brokers.yaml")
	if err := testBrokers().Save(path); err != nil {
		t.Fatal(err)
	}
	dead := []Item{{Kind: Dead, Email: "privacy@acme.example", Subject: "bounce", Broker: testBrokers().Brokers[0]}}
	cleared, err := Clear(path, dead)
	if err != nil || len(cleared) != 1 {
		t.Fatalf("Clear = %v, %v", cleared, err)
	}
	db, _ := broker.LoadFromFile(path)
	if acme := db.FindByID("acme"); acme.Email != "" || !strings.Contains(acme.Notes, "undeliverable") {
		t.Errorf("acme = %+v", acme)
	}
	if again, _ := Clear(path, dead); len(again) != 0 {
		t.Error("an already cleared address must not be cleared twice")
	}
	if _, err := Clear(filepath.Join(t.TempDir(), "missing.yaml"), dead); err == nil {
		t.Error("a missing list must be an error")
	}
}

func TestClearWithoutAListWritesTheUsersOwnEntries(t *testing.T) {
	testenv.SetHome(t, t.TempDir())
	// A broker from the shipped list, so Clear finds it by email.
	shipped, err := broker.Load("")
	if err != nil {
		t.Fatal(err)
	}
	var target broker.Broker
	for _, b := range shipped.Brokers {
		if b.Email != "" {
			target = b
			break
		}
	}
	cleared, err := Clear("", []Item{{Kind: Dead, Email: target.Email, Subject: "bounce", Broker: target}})
	if err != nil || len(cleared) != 1 {
		t.Fatalf("Clear = %v, %v", cleared, err)
	}
	local, err := broker.LoadLocal()
	if err != nil || local.FindByID(target.ID) == nil || local.FindByID(target.ID).Email != "" {
		t.Errorf("the correction was not saved as the user's own entry: %v", err)
	}
}

func TestEverSentCoversEveryProfile(t *testing.T) {
	store, err := history.NewStore(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	for profile, id := range map[string]string{"a": "acme", "b": "globex"} {
		if err := store.Add(&history.Record{ProfileID: profile, BrokerID: id, BrokerName: id, Email: id + "@x.example", Template: "gdpr", Status: history.StatusSent, SentAt: time.Now()}); err != nil {
			t.Fatal(err)
		}
	}
	cfg := &config.Config{Profiles: []config.NamedProfile{{ID: "a"}, {ID: "b"}}}
	got := EverSent(cfg, store)
	if !got["acme"] || !got["globex"] || len(got) != 2 {
		t.Errorf("EverSent = %v", got)
	}
	_ = store.Close()
	if got := EverSent(cfg, store); len(got) != 0 {
		t.Errorf("a broken store must yield nothing, got %v", got)
	}
}
