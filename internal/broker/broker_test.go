package broker

import (
	"strings"
	"testing"
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

func TestMarkEmailUnreachableClearsEmailAndKeepsRecord(t *testing.T) {
	db := &BrokerDatabase{
		Brokers: []Broker{
			{ID: "spokeo", Name: "Spokeo", Email: "privacy@spokeo.com", Website: "https://spokeo.com", OptOutURL: "https://spokeo.com/optout", Region: "us", Category: "people-search"},
		},
	}

	got := db.MarkEmailUnreachable("privacy@spokeo.com", "mailbox full")
	if got == nil {
		t.Fatal("MarkEmailUnreachable returned nil for a known email")
	}

	b := db.FindByID("spokeo")
	if b.Email != "" {
		t.Errorf("Email = %q, want cleared", b.Email)
	}
	if b.Name != "Spokeo" || b.Website != "https://spokeo.com" || b.OptOutURL != "https://spokeo.com/optout" || b.Category != "people-search" {
		t.Errorf("MarkEmailUnreachable altered fields other than Email/Notes: %+v", b)
	}
	if !strings.Contains(b.Notes, "privacy@spokeo.com") || !strings.Contains(b.Notes, "mailbox full") {
		t.Errorf("Notes = %q, want it to mention the old address and reason", b.Notes)
	}
	if len(db.Brokers) != 1 {
		t.Errorf("len(db.Brokers) = %d, want 1 (broker must not be removed)", len(db.Brokers))
	}
}

func TestMarkEmailUnreachableMergesWithExistingNotes(t *testing.T) {
	db := &BrokerDatabase{
		Brokers: []Broker{
			{ID: "spokeo", Email: "privacy@spokeo.com", Notes: "Confirmed EU region."},
		},
	}

	db.MarkEmailUnreachable("privacy@spokeo.com", "hard bounce")

	b := db.FindByID("spokeo")
	if !strings.Contains(b.Notes, "Confirmed EU region.") {
		t.Errorf("Notes = %q, want existing note preserved", b.Notes)
	}
	if !strings.Contains(b.Notes, "privacy@spokeo.com") {
		t.Errorf("Notes = %q, want new bounce note appended", b.Notes)
	}
}

func TestMarkEmailUnreachableUnknownEmailReturnsNil(t *testing.T) {
	db := testDB()

	got := db.MarkEmailUnreachable("nobody@example.com", "bounce")
	if got != nil {
		t.Errorf("MarkEmailUnreachable(unknown email) = %+v, want nil", got)
	}
}
