package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/drumandbytes/eraser/internal/broker"
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
		"Includes a ready-to-send request letter.", // description mentions the rendered letters
		"https://acme.example/opt-out",
		"## At a glance", "People-search site", "United States (CCPA", "Web form, with an email fallback",
		"## About people-search sites", "## Related reading",
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
	jb, err := os.ReadFile(filepath.Join(dir, "site", "static", "brokers.json"))
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

func TestRelatedGuides(t *testing.T) {
	mk := func(id, cat string) broker.Broker {
		return broker.Broker{ID: id, Name: strings.ToUpper(id), Category: cat}
	}
	curated := []broker.Broker{mk("a", "people-search"), mk("b", "people-search"), mk("c", "background-check"), mk("d", "people-search"), mk("e", "people-search")}

	// Next ones in name order within the category, wrapping, never itself or another category.
	got := relatedGuides(curated[3], curated, 5)
	if len(got) != 3 || got[0].ID != "e" || got[1].ID != "a" || got[2].ID != "b" {
		t.Errorf("related for d = %+v, want e, a, b", got)
	}
	if got := relatedGuides(curated[2], curated, 5); len(got) != 0 {
		t.Errorf("a category of one should have no related guides, got %+v", got)
	}
	if got := relatedGuides(curated[0], curated, 2); len(got) != 2 {
		t.Errorf("limit not applied: %+v", got)
	}
}

func TestGuidesResearchedFields(t *testing.T) {
	dir := t.TempDir()
	brokersPath := filepath.Join(dir, "brokers.yaml")
	if err := os.WriteFile(brokersPath, []byte(`brokers:
  - id: adco
    name: Ad Co
    email: privacy@adco.example
    region: us
    category: marketing
    guide: true
    parent: Ad Holdings Inc.
    hq: Austin, USA
    holds: email addresses and device IDs
    verification: none stated
    response_time: 45 days
    sources: ["https://adco.example/privacy", "not a url"]
    researched: "2026-10-25"
  - id: plain
    name: Plain Co
    region: us
    category: marketing
`), 0o600); err != nil {
		t.Fatal(err)
	}
	defer func(b string) { brokerFile = b }(brokerFile)
	brokerFile = brokersPath

	out := filepath.Join(dir, "site", "content")
	if err := runGuides(out, "md"); err != nil {
		t.Fatalf("runGuides: %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, "brokers", "plain.md")); !os.IsNotExist(err) {
		t.Errorf("a long-tail broker without guide: true must not get a page")
	}
	b, err := os.ReadFile(filepath.Join(out, "brokers", "adco.md"))
	if err != nil {
		t.Fatalf("guide: true broker has no page: %v", err)
	}
	s := string(b)
	for _, want := range []string{
		"**Operated by:** Ad Holdings Inc., Austin, USA", "**Data they say they hold:** email addresses and device IDs",
		"**Identity check:** none stated", "**Response time in their policy:** 45 days",
		"## Sources", "read on 2026-10-25", "<https://adco.example/privacy>", "## About marketing-data brokers",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in:\n%s", want, s)
		}
	}
	if strings.Contains(s, "not a url") {
		t.Errorf("an invalid source URL should be dropped by sanitizing:\n%s", s)
	}
}
