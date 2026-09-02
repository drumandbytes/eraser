package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunGuidesMarkdown(t *testing.T) {
	dir := t.TempDir()
	brokersPath := filepath.Join(dir, "brokers.yaml")
	if err := os.WriteFile(brokersPath, []byte(`brokers:
  - id: acme
    name: Acme People Finder
    email: privacy@acme.example
    opt_out_url: https://acme.example/opt-out
    region: us
    category: people-search
    notes: Replied 2026-08-01, removed within a week.
  - id: globex-ads
    name: Globex Ads
    email: dpo@globex.example
    region: eu
    category: marketing
  - id: nocontact
    name: No Contact Bureau
    region: us
    category: background-check
`), 0o600); err != nil {
		t.Fatal(err)
	}
	defer func(b string) { brokerFile = b }(brokerFile)
	brokerFile = brokersPath

	out := filepath.Join(dir, "site", "content")
	if err := runGuides(out, "md"); err != nil {
		t.Fatalf("runGuides: %v", err)
	}

	// Curated: people-search + background-check get pages; marketing does not.
	if _, err := os.Stat(filepath.Join(out, "brokers", "acme.md")); err != nil {
		t.Errorf("acme.md missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, "brokers", "nocontact.md")); err != nil {
		t.Errorf("nocontact.md missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, "brokers", "globex-ads.md")); !os.IsNotExist(err) {
		t.Errorf("marketing broker should not get a page")
	}

	acme, _ := os.ReadFile(filepath.Join(out, "brokers", "acme.md"))
	s := string(acme)
	for _, want := range []string{
		`title: "How to opt out of Acme People Finder"`,
		`last_checked: "2026-08-01"`,
		"https://acme.example/opt-out",
		"Article 17",        // GDPR body rendered
		"1798.105",          // CCPA body rendered
		"[your first name]", // placeholder, not a real profile
	} {
		if !strings.Contains(s, want) {
			t.Errorf("acme.md missing %q", want)
		}
	}

	// nocontact has no email/URL -> "no public opt-out" wording.
	nc, _ := os.ReadFile(filepath.Join(out, "brokers", "nocontact.md"))
	if !strings.Contains(string(nc), "No public opt-out") {
		t.Errorf("nocontact.md should note the missing contact info")
	}

	// Directory JSON has every broker, curated flag set correctly.
	jb, err := os.ReadFile(filepath.Join(dir, "site", "data", "brokers.json"))
	if err != nil {
		t.Fatal(err)
	}
	var entries []struct {
		ID      string `json:"id"`
		Curated bool   `json:"curated"`
	}
	if err := json.Unmarshal(jb, &entries); err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("directory has %d entries, want 3", len(entries))
	}
	byID := map[string]bool{}
	for _, e := range entries {
		byID[e.ID] = e.Curated
	}
	if !byID["acme"] || byID["globex-ads"] {
		t.Errorf("curated flags wrong: %+v", byID)
	}
}

func TestRunGuidesBadFormat(t *testing.T) {
	if err := runGuides(t.TempDir(), "pdf"); err == nil {
		t.Error("expected error for unknown format")
	}
}
