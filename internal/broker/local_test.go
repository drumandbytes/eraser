package broker

import (
	"os"
	"path/filepath"
	"testing"
)

// Your own entries survive update-brokers replacing the list, override a
// published entry by id instead of duplicating it, and apply to every list
// except an explicit --brokers file.
func TestLocalEntriesMergeOverEveryList(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	base, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveLocal(Broker{ID: "corner-shop", Name: "Corner Shop", Email: "privacy@corner.example", Region: "eu"}); err != nil {
		t.Fatal(err)
	}
	override := *base.FindByID("spokeo")
	override.Email = "new-privacy@spokeo.example"
	if err := SaveLocal(override); err != nil {
		t.Fatal(err)
	}

	check := func(name string, db *BrokerDatabase, err error, wantLen int) {
		t.Helper()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if db.FindByID("corner-shop") == nil || db.FindByID("spokeo").Email != "new-privacy@spokeo.example" || len(db.Brokers) != wantLen {
			t.Errorf("%s: local entries not merged (len %d, want %d)", name, len(db.Brokers), wantLen)
		}
	}
	db, err := Load("")
	check("embedded list", db, err, len(base.Brokers)+1)

	// what update-brokers writes: replaces ~/.eraser/brokers.yaml wholesale
	updated := filepath.Join(home, ".eraser", "brokers.yaml")
	if err := (&BrokerDatabase{Brokers: base.Brokers[:300]}).Save(updated); err != nil {
		t.Fatal(err)
	}
	db, err = Load("")
	check("after update-brokers", db, err, 301)

	verified, err := LoadList("", "", "verified")
	if err != nil || verified.FindByID("corner-shop") == nil {
		t.Errorf("verified list: own entry missing (%v)", err)
	}

	explicit, err := Load(updated)
	if err != nil || explicit.FindByID("corner-shop") != nil {
		t.Errorf("explicit --brokers file must be used as-is (%v)", err)
	}

	// re-saving the same id replaces, never duplicates
	if err := SaveLocal(Broker{ID: "corner-shop", Name: "Corner Shop Ltd", Region: "eu"}); err != nil {
		t.Fatal(err)
	}
	local, _ := LoadLocal()
	if len(local.Brokers) != 2 || local.FindByID("corner-shop").Name != "Corner Shop Ltd" {
		t.Errorf("local file = %+v", local.Brokers)
	}

	if err := os.WriteFile(LocalPath(), []byte("brokers: [oops"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(""); err == nil {
		t.Error("a broken local file must be reported, not silently ignored")
	}
}

func TestNewID(t *testing.T) {
	for in, want := range map[string]string{"Acme Data GmbH": "acme-data-gmbh", "Corner Shop OÜ": "corner-shop-ou", "Šķēle & Straße Ø": "skele-strasse-o", "": ""} {
		if got := NewID(in); got != want {
			t.Errorf("NewID(%q) = %q, want %q", in, got, want)
		}
	}
}
