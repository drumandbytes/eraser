package main

import (
	"net/mail"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eraser-privacy/eraser/internal/broker"
	"github.com/eraser-privacy/eraser/internal/history"
	emailtmpl "github.com/eraser-privacy/eraser/internal/template"
)

func manualBrokerDB() *broker.BrokerDatabase {
	return &broker.BrokerDatabase{Brokers: []broker.Broker{
		{ID: "acme", Name: "Acme", Email: "privacy@acme.example", Region: "us", Category: "people-search"},
		{ID: "globex", Name: "Globex", Email: "dpo@globex.example", Region: "eu", Category: "marketing"},
		{ID: "noemail", Name: "NoEmail", Region: "us", Category: "marketing", OptOutURL: "https://noemail.example/opt-out"},
	}}
}

func TestSelectBrokers(t *testing.T) {
	db := manualBrokerDB()

	got, err := selectBrokers(db, []string{"acme", "globex"}, "", "")
	if err != nil || len(got) != 2 {
		t.Fatalf("by id: %v, %d", err, len(got))
	}
	if _, err := selectBrokers(db, []string{"nope"}, "", ""); err == nil {
		t.Error("unknown id should error")
	}

	got, err = selectBrokers(db, nil, "eu", "")
	if err != nil || len(got) != 1 || got[0].ID != "globex" {
		t.Fatalf("region filter: %v, %+v", err, got)
	}
	got, _ = selectBrokers(db, nil, "", "marketing")
	if len(got) != 2 {
		t.Fatalf("category filter got %d, want 2", len(got))
	}
	got, _ = selectBrokers(db, nil, "", "")
	if len(got) != 3 {
		t.Fatalf("no filter got %d, want all 3", len(got))
	}
}

func TestFormatEML(t *testing.T) {
	eml := formatEML("me@example.com", "them@example.com", &emailtmpl.Email{Subject: "Hi there", Body: "line one\nline two"})
	msg, err := mail.ReadMessage(strings.NewReader(string(eml)))
	if err != nil {
		t.Fatalf("not a valid RFC-822 message: %v", err)
	}
	if msg.Header.Get("To") != "them@example.com" || msg.Header.Get("Subject") != "Hi there" {
		t.Errorf("headers = %+v", msg.Header)
	}
	if !strings.Contains(string(eml), "\r\nline one\r\nline two\r\n") {
		t.Errorf("body not CRLF-normalised: %q", string(eml))
	}
}

// writeManualFixtures drops a config + brokers file in a temp dir and points
// the global flag vars at them (restored on cleanup).
func writeManualFixtures(t *testing.T) (cfgPath string) {
	t.Helper()
	dir := t.TempDir()
	cfgPath = filepath.Join(dir, "config.yaml")
	brokersPath := filepath.Join(dir, "brokers.yaml")

	if err := os.WriteFile(brokersPath, []byte(`brokers:
  - id: acme
    name: Acme
    email: privacy@acme.example
    region: us
    category: people-search
  - id: noemail
    name: NoEmail
    region: us
    category: marketing
`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfgPath, []byte(`profile:
  first_name: Jane
  last_name: Doe
  email: jane@example.com
options:
  template: gdpr
  send_mode: manual
`), 0o600); err != nil {
		t.Fatal(err)
	}

	old1, old2 := cfgFile, brokerFile
	cfgFile, brokerFile = cfgPath, brokersPath
	t.Cleanup(func() { cfgFile, brokerFile = old1, old2 })
	return cfgPath
}

func TestRunDraftToDir(t *testing.T) {
	writeManualFixtures(t)
	out := t.TempDir()
	if err := runDraft(nil, out, "", ""); err != nil {
		t.Fatalf("runDraft: %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, "acme.eml")); err != nil {
		t.Errorf("acme.eml not written: %v", err)
	}
	// noemail has no address - it must be skipped, not written.
	if _, err := os.Stat(filepath.Join(out, "noemail.eml")); !os.IsNotExist(err) {
		t.Errorf("noemail.eml should not exist")
	}
}

func TestRunMarkSentRecordsManual(t *testing.T) {
	cfgPath := writeManualFixtures(t)

	if err := runMarkSent([]string{"acme"}, "", "", false); err != nil {
		t.Fatalf("runMarkSent: %v", err)
	}

	store, err := history.NewStore(history.DBPathFor(cfgPath))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	recs, err := store.GetAllRequests(history.DefaultProfileID)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 || recs[0].BrokerID != "acme" || recs[0].SentMethod != "manual" || recs[0].Status != history.StatusSent {
		t.Fatalf("recorded rows = %+v", recs)
	}

	// dry-run writes nothing new.
	if err := runMarkSent([]string{"acme"}, "", "", true); err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	recs, _ = store.GetAllRequests(history.DefaultProfileID)
	if len(recs) != 1 {
		t.Errorf("dry-run added a row: %d total", len(recs))
	}
}
