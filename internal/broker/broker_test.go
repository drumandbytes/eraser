package broker

import "testing"

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
