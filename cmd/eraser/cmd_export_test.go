package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/drumandbytes/eraser/internal/evidence"
	"github.com/drumandbytes/eraser/internal/history"
)

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
	if strings.Contains(string(jsonData), "null") {
		t.Errorf("export JSON contains null (empty lists must be []):\n%s", jsonData)
	}
	var rep evidence.EvidenceReport
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
