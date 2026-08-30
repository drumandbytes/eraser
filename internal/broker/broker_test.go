package broker

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func testDB() *BrokerDatabase {
	return &BrokerDatabase{
		Brokers: []Broker{
			{ID: "spokeo", Name: "Spokeo", Region: "us", Category: "people-search"},
			{ID: "altisource-holdings", Name: "Altisource Holdings, LLC", Region: "us", Category: "requires-id"},
			{ID: "vistar-media", Name: "Vistar Media, Inc.", Region: "us", Category: "device-id-only"},
			{ID: "creditsafe", Name: "Creditsafe", Region: "eu", Category: "financial-b2b"},
			{ID: "global-broker", Name: "Global Broker", Region: "global", Category: "marketing"},
		},
	}
}

func TestFilterExcludedCategories(t *testing.T) {
	db := testDB()

	got := db.Filter(nil, nil, []string{"requires-id"})

	for _, b := range got {
		if b.ID == "altisource-holdings" {
			t.Errorf("Filter with excludedCategories=[requires-id] still returned %q", b.ID)
		}
	}
	if len(got) != len(db.Brokers)-1 {
		t.Errorf("Filter with excludedCategories=[requires-id] returned %d brokers, want %d", len(got), len(db.Brokers)-1)
	}
}

func TestFilterExcludedCategoriesIsCaseInsensitive(t *testing.T) {
	db := testDB()

	got := db.Filter(nil, nil, []string{"Requires-ID", "DEVICE-ID-ONLY"})

	for _, b := range got {
		if b.ID == "altisource-holdings" || b.ID == "vistar-media" {
			t.Errorf("case-insensitive category exclusion missed %q", b.ID)
		}
	}
	if len(got) != len(db.Brokers)-2 {
		t.Errorf("Filter returned %d brokers, want %d", len(got), len(db.Brokers)-2)
	}
}

func TestFilterExcludedCategoriesCombinesWithExistingFilters(t *testing.T) {
	db := testDB()

	// Region filter (us + global) combined with a category exclusion -
	// both conditions must apply together, not override each other.
	got := db.Filter([]string{"us"}, nil, []string{"requires-id"})

	want := map[string]bool{"spokeo": true, "vistar-media": true, "global-broker": true}
	if len(got) != len(want) {
		t.Fatalf("Filter(us, nil, [requires-id]) returned %d brokers, want %d: %+v", len(got), len(want), got)
	}
	for _, b := range got {
		if !want[b.ID] {
			t.Errorf("unexpected broker %q in filtered result", b.ID)
		}
	}
}

func TestFilterNoExclusionsReturnsEverything(t *testing.T) {
	db := testDB()

	got := db.Filter(nil, nil, nil)

	if len(got) != len(db.Brokers) {
		t.Errorf("Filter(nil, nil, nil) returned %d brokers, want all %d", len(got), len(db.Brokers))
	}
}

func TestNormalizePriority(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"high", "high"},
		{"medium", "medium"},
		{"low", "low"},
		{"HIGH", "high"},
		{"  Medium  ", "medium"},
		{"", ""},
		{"urgent", ""},
		{"highest", ""},
	}

	for _, tt := range tests {
		if got := NormalizePriority(tt.in); got != tt.want {
			t.Errorf("NormalizePriority(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestFilterByPriority(t *testing.T) {
	brokers := []Broker{
		{ID: "spokeo", Priority: "high"},
		{ID: "acxiom", Priority: "High"},
		{ID: "smalltown", Priority: "low"},
		{ID: "untagged"},
	}

	tests := []struct {
		name       string
		priorities []string
		want       []string
	}{
		{"nil means all", nil, []string{"spokeo", "acxiom", "smalltown", "untagged"}},
		{"empty means all", []string{}, []string{"spokeo", "acxiom", "smalltown", "untagged"}},
		{"high, case-insensitive on both sides", []string{"HIGH"}, []string{"spokeo", "acxiom"}},
		{"multiple tiers", []string{"high", "low"}, []string{"spokeo", "acxiom", "smalltown"}},
		{"an explicit filter excludes untagged brokers", []string{"medium"}, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var ids []string
			for _, b := range FilterByPriority(brokers, tt.priorities) {
				ids = append(ids, b.ID)
			}
			if !reflect.DeepEqual(ids, tt.want) {
				t.Errorf("FilterByPriority(%v) = %v, want %v", tt.priorities, ids, tt.want)
			}
		})
	}
}

func TestSortByPriorityIsStable(t *testing.T) {
	brokers := []Broker{
		{ID: "low-1", Priority: "low"},
		{ID: "untagged-1"},
		{ID: "high-1", Priority: "high"},
		{ID: "low-2", Priority: "low"},
		{ID: "medium-1", Priority: "medium"},
		{ID: "high-2", Priority: "high"},
	}

	SortByPriority(brokers)

	var ids []string
	for _, b := range brokers {
		ids = append(ids, b.ID)
	}
	want := []string{"high-1", "high-2", "medium-1", "low-1", "low-2", "untagged-1"}
	if !reflect.DeepEqual(ids, want) {
		t.Errorf("SortByPriority produced %v, want %v", ids, want)
	}
}

// TestLoadDoesNotRewritePriority guards the reason sanitizeBroker deliberately
// leaves Priority alone: Save rewrites data/brokers.yaml from the in-memory
// copy, so normalizing on load would erase a tag the code doesn't recognize
// from every entry the next time add-broker or cleanup-bounces saves.
func TestLoadDoesNotRewritePriority(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "brokers.yaml")
	original := "brokers:\n    - id: custom\n      name: Custom\n      email: a@b.com\n      region: us\n      priority: critical\n"
	if err := os.WriteFile(path, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}

	db, err := LoadFromFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if db.Brokers[0].Priority != "critical" {
		t.Errorf("load rewrote an unrecognized priority to %q, want it left as %q", db.Brokers[0].Priority, "critical")
	}

	if err := db.Save(path); err != nil {
		t.Fatal(err)
	}
	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(saved), "priority: critical") {
		t.Errorf("Save dropped the unrecognized priority; file is now:\n%s", saved)
	}
}

// TestShippedBrokerDatabasePriorities checks data/brokers.yaml itself: every
// entry needs a valid priority or the web UI's priority selector silently
// hides it. Reads the raw YAML rather than going through LoadFromFile so a
// case or whitespace typo can't be normalized away before the assertion.
func TestShippedBrokerDatabasePriorities(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "data", "brokers.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	var db BrokerDatabase
	if err := yaml.Unmarshal(data, &db); err != nil {
		t.Fatal(err)
	}
	if len(db.Brokers) == 0 {
		t.Fatal("no brokers loaded from data/brokers.yaml")
	}

	valid := make(map[string]bool, len(Priorities))
	for _, p := range Priorities {
		valid[p] = true
	}

	for _, b := range db.Brokers {
		if !valid[b.Priority] {
			t.Errorf("broker %q has priority %q, want one of %v", b.ID, b.Priority, Priorities)
		}
	}
}

// TestNonBrokerEntriesHaveNoEmail enforces the invariant the "non-broker"
// category depends on. Those entries are search engines, industry
// preference services and suppression registries - things you act on
// yourself through a web form, not parties you send an erasure request to.
// Giving one an address would put it in a bulk send's target list and mail
// an erasure demand to, say, the FTC's Do Not Call registry.
func TestNonBrokerEntriesHaveNoEmail(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "data", "brokers.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	var db BrokerDatabase
	if err := yaml.Unmarshal(data, &db); err != nil {
		t.Fatal(err)
	}

	found := 0
	for _, b := range db.Brokers {
		if b.Category != "non-broker" {
			continue
		}
		found++
		if strings.TrimSpace(b.Email) != "" {
			t.Errorf("non-broker entry %q has email %q; non-brokers must not be emailable", b.ID, b.Email)
		}
		if b.Notes == "" {
			t.Errorf("non-broker entry %q has no notes; it needs to say what to do there instead", b.ID)
		}
	}
	if found == 0 {
		t.Error("no non-broker entries found - did the category get renamed?")
	}
}

// TestShippedBrokerDatabaseHasNoDuplicateEmails guards against the same
// address appearing on two entries, which would mail that desk twice in a
// single run. Two entries legitimately sharing a *domain* is fine and
// common - separate registered entities under one corporate site, each with
// its own privacy contact - so this checks the address, not the domain.
//
// This caught two real duplicates introduced by a bulk EU import that
// deduplicated on broker ID only: Equifax Ltd against the existing Equifax
// entry (both on the UK DPO address), and Verband der Vereine Creditreform
// against the existing Creditreform entry.
func TestShippedBrokerDatabaseHasNoDuplicateEmails(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "data", "brokers.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	var db BrokerDatabase
	if err := yaml.Unmarshal(data, &db); err != nil {
		t.Fatal(err)
	}

	seen := make(map[string]string, len(db.Brokers))
	for _, b := range db.Brokers {
		email := strings.ToLower(strings.TrimSpace(b.Email))
		if email == "" {
			continue
		}
		if first, dup := seen[email]; dup {
			t.Errorf("brokers %q and %q share the email %s; a send run would email that address twice", first, b.ID, email)
			continue
		}
		seen[email] = b.ID
	}
}
