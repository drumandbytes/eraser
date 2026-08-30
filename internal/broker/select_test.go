package broker

import "testing"

func selectTestDB() []Broker {
	return []Broker{
		{ID: "spokeo", Name: "Spokeo", Category: "people-search"},
		{ID: "acxiom", Name: "Acxiom", Category: "marketing"},
		{ID: "epsilon", Name: "Epsilon Data Management", Category: "marketing"},
		{ID: "checkr", Name: "Checkr", Category: "Background-Check"},
		{ID: "nocat", Name: "No Category"},
	}
}

func ids(list []Broker) []string {
	out := make([]string, len(list))
	for i, b := range list {
		out[i] = b.ID
	}
	return out
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestSelectCategories(t *testing.T) {
	list := selectTestDB()

	// Empty selection must not filter anything out - it means "no --category
	// was passed", not "match nothing".
	if got := SelectCategories(list, nil); len(got) != len(list) {
		t.Errorf("empty categories returned %d brokers, want all %d", len(got), len(list))
	}

	if got := ids(SelectCategories(list, []string{"people-search"})); !equal(got, []string{"spokeo"}) {
		t.Errorf("people-search = %v, want [spokeo]", got)
	}

	// Multiple categories, as a comma-separated --category would produce.
	got := ids(SelectCategories(list, []string{"people-search", "marketing"}))
	if !equal(got, []string{"spokeo", "acxiom", "epsilon"}) {
		t.Errorf("two categories = %v, want [spokeo acxiom epsilon]", got)
	}

	// Case-insensitive on both sides: brokers.yaml is community-edited and
	// "Background-Check" appears with varying case.
	if got := ids(SelectCategories(list, []string{"BACKGROUND-CHECK"})); !equal(got, []string{"checkr"}) {
		t.Errorf("case-insensitive match = %v, want [checkr]", got)
	}

	if got := SelectCategories(list, []string{"nope"}); len(got) != 0 {
		t.Errorf("unknown category returned %d brokers, want 0", len(got))
	}
}

func TestSelectIDs(t *testing.T) {
	list := selectTestDB()

	if got := SelectIDs(list, nil); len(got) != len(list) {
		t.Errorf("empty ids returned %d brokers, want all %d", len(got), len(list))
	}

	if got := ids(SelectIDs(list, []string{"acxiom", "epsilon"})); !equal(got, []string{"acxiom", "epsilon"}) {
		t.Errorf("by id = %v, want [acxiom epsilon]", got)
	}

	// Name is accepted alongside ID, matching Filter's excluded-broker rules.
	if got := ids(SelectIDs(list, []string{"Epsilon Data Management"})); !equal(got, []string{"epsilon"}) {
		t.Errorf("by name = %v, want [epsilon]", got)
	}

	if got := ids(SelectIDs(list, []string{"SPOKEO"})); !equal(got, []string{"spokeo"}) {
		t.Errorf("case-insensitive id = %v, want [spokeo]", got)
	}

	if got := SelectIDs(list, []string{"missing"}); len(got) != 0 {
		t.Errorf("unknown id returned %d brokers, want 0", len(got))
	}
}

// The two selectors compose, and must run after Filter's excludes so a
// broker excluded in config stays excluded even when named explicitly.
func TestSelectComposesWithFilter(t *testing.T) {
	db := &BrokerDatabase{Brokers: selectTestDB()}

	filtered := db.Filter(nil, []string{"acxiom"}, nil)
	got := ids(SelectIDs(filtered, []string{"acxiom", "epsilon"}))
	if !equal(got, []string{"epsilon"}) {
		t.Errorf("got %v, want [epsilon] - an excluded broker must stay excluded", got)
	}
}

func TestCategoriesListsDistinctInOrder(t *testing.T) {
	db := &BrokerDatabase{Brokers: selectTestDB()}
	got := db.Categories()
	want := []string{"people-search", "marketing", "background-check"}
	if !equal(got, want) {
		t.Errorf("Categories() = %v, want %v (distinct, first-seen order, blanks skipped)", got, want)
	}
}
