package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/drumandbytes/eraser/internal/broker"
	"github.com/drumandbytes/eraser/internal/config"
	"github.com/drumandbytes/eraser/internal/history"
	emailtmpl "github.com/drumandbytes/eraser/internal/template"
)

func testProfile() config.Profile {
	return config.Profile{
		FirstName:    "Jane",
		LastName:     "Doe",
		Email:        "jane@example.com",
		Address:      "1 Main St",
		City:         "Riga",
		Country:      "Latvia",
		DateOfBirth:  "1990-01-01",
		NameVariants: []string{"J. Doe"},
	}
}

func testBrokerDB() *broker.BrokerDatabase {
	return &broker.BrokerDatabase{Brokers: []broker.Broker{
		{ID: "acme", Name: "Acme Data", Email: "privacy@acme.example", Website: "https://acme.example", Region: "us", Category: "people-search"},
		{ID: "globex", Name: "Globex", Email: "dpo@globex.example", Region: "eu", Category: "marketing"},
	}}
}

func TestBuildEvidenceReport(t *testing.T) {
	engine, err := emailtmpl.NewEngine()
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)

	requests := []history.Record{
		{ // sent 3 months ago, never answered -> past deadline
			BrokerID: "acme", BrokerName: "Acme Data", Email: "privacy@acme.example",
			Template: "gdpr", Status: history.StatusSent,
			SentAt: now.AddDate(0, -3, 0), MessageID: "<abc@mail>",
			PipelineStatus: history.PipelineAwaitingResponse,
		},
		{ // sent 3 days ago, answered -> not past deadline
			BrokerID: "globex", BrokerName: "Globex", Email: "dpo@globex.example",
			Template: "gdpr", Status: history.StatusSent, SentAt: now.AddDate(0, 0, -3),
		},
	}
	responses := []history.BrokerResponse{
		{BrokerID: "globex", BrokerName: "Globex", ResponseType: "success",
			EmailFrom: "dpo@globex.example", EmailSubject: "Done", ReceivedAt: now.AddDate(0, 0, -1)},
	}

	rep := buildEvidenceReport(testProfile(), requests, responses, testBrokerDB(), engine, time.Time{}, now)

	if rep.Subject.FullName != "Jane Doe" {
		t.Errorf("subject name = %q", rep.Subject.FullName)
	}
	if rep.Summary.Sent != 2 || rep.Summary.BrokersContacted != 2 || rep.Summary.BrokersResponded != 1 {
		t.Errorf("summary = %+v", rep.Summary)
	}
	if len(rep.Summary.PastDeadline) != 1 || rep.Summary.PastDeadline[0] != "Acme Data" {
		t.Errorf("past deadline = %v, want [Acme Data]", rep.Summary.PastDeadline)
	}

	var acme, globex *BrokerEvidence
	for i := range rep.Brokers {
		switch rep.Brokers[i].BrokerID {
		case "acme":
			acme = &rep.Brokers[i]
		case "globex":
			globex = &rep.Brokers[i]
		}
	}
	if acme == nil || globex == nil {
		t.Fatal("missing broker evidence")
	}
	if !acme.PastDeadline {
		t.Error("acme should be past deadline")
	}
	if globex.PastDeadline {
		t.Error("globex should not be past deadline (it responded)")
	}
	if len(acme.Requests) != 1 || acme.Requests[0].MessageID != "<abc@mail>" {
		t.Errorf("acme request = %+v", acme.Requests)
	}
	if !strings.Contains(acme.Requests[0].LegalBasis, "Article 17") {
		t.Errorf("legal basis = %q", acme.Requests[0].LegalBasis)
	}
	if !strings.Contains(acme.Requests[0].RenderedBody, "erasure") {
		t.Errorf("reconstructed body missing expected text: %q", acme.Requests[0].RenderedBody)
	}
	if acme.Requests[0].RenderedSubject == "" {
		t.Error("reconstructed subject empty")
	}
}

func TestBuildEvidenceReportSinceFilter(t *testing.T) {
	engine, _ := emailtmpl.NewEngine()
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	requests := []history.Record{
		{BrokerID: "acme", Template: "gdpr", Status: history.StatusSent, SentAt: now.AddDate(0, -6, 0)},
		{BrokerID: "globex", Template: "gdpr", Status: history.StatusSent, SentAt: now.AddDate(0, 0, -2)},
	}
	rep := buildEvidenceReport(testProfile(), requests, nil, testBrokerDB(), engine, now.AddDate(0, -1, 0), now)
	if rep.Summary.TotalRequests != 1 || rep.Summary.BrokersContacted != 1 {
		t.Errorf("since filter not applied: %+v", rep.Summary)
	}
	if rep.Brokers[0].BrokerID != "globex" {
		t.Errorf("wrong broker survived filter: %s", rep.Brokers[0].BrokerID)
	}
}

func TestRunExportEndToEnd(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	brokersPath := filepath.Join(dir, "brokers.yaml")

	if err := os.WriteFile(brokersPath, []byte(`brokers:
  - id: acme
    name: Acme Data
    email: privacy@acme.example
    region: us
    category: people-search
`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfgPath, []byte(`profile:
  first_name: Jane
  last_name: Doe
  email: jane@example.com
email:
  provider: smtp
  from: jane@example.com
  smtp:
    host: smtp.example.com
    port: 465
    username: jane@example.com
    password: xxxxxxxxxxxxxxxx
    use_tls: true
options:
  template: gdpr
`), 0o600); err != nil {
		t.Fatal(err)
	}

	store, err := history.NewStore(history.DBPathFor(cfgPath))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Add(&history.Record{
		BrokerID: "acme", BrokerName: "Acme Data", Email: "privacy@acme.example",
		Template: "gdpr", Status: history.StatusSent, SentAt: time.Now().AddDate(0, -2, 0),
	}); err != nil {
		t.Fatal(err)
	}
	_ = store.Close()

	// Point the global flag vars at the fixtures (restored after).
	defer func(c, b string) { cfgFile, brokerFile = c, b }(cfgFile, brokerFile)
	cfgFile, brokerFile = cfgPath, brokersPath

	htmlOut := filepath.Join(dir, "ev.html")
	if err := runExport(exportOptions{output: htmlOut, format: "html"}); err != nil {
		t.Fatalf("runExport html: %v", err)
	}
	htmlData, err := os.ReadFile(htmlOut)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Jane Doe", "Acme Data", "PAST DEADLINE", "Article 17"} {
		if !strings.Contains(string(htmlData), want) {
			t.Errorf("html output missing %q", want)
		}
	}
	if info, _ := os.Stat(htmlOut); info != nil && info.Mode().Perm() != 0o600 {
		t.Errorf("output perms = %v, want 0600", info.Mode().Perm())
	}

	jsonOut := filepath.Join(dir, "ev.json")
	if err := runExport(exportOptions{output: jsonOut, format: "json"}); err != nil {
		t.Fatalf("runExport json: %v", err)
	}
	jsonData, err := os.ReadFile(jsonOut)
	if err != nil {
		t.Fatal(err)
	}
	var rep EvidenceReport
	if err := json.Unmarshal(jsonData, &rep); err != nil {
		t.Fatalf("json round-trip: %v", err)
	}
	if rep.Summary.Sent != 1 || len(rep.Summary.PastDeadline) != 1 {
		t.Errorf("json summary = %+v", rep.Summary)
	}

	if err := runExport(exportOptions{output: filepath.Join(dir, "x"), format: "xml"}); err == nil {
		t.Error("expected error for unknown format")
	}
}
