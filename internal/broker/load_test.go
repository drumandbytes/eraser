package broker

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseAndEmbeddedLoad(t *testing.T) {
	// The embedded default must parse and carry a sane number of entries.
	db, err := Load("")
	if err != nil {
		t.Fatalf("Load(embedded): %v", err)
	}
	if len(db.Brokers) < 200 {
		t.Fatalf("embedded broker list has only %d entries", len(db.Brokers))
	}
}

func TestLoadPrecedence(t *testing.T) {
	// HOME override so UserBrokersPath() points into a temp dir.
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	// No user copy, no override -> embedded.
	embedded, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	embeddedN := len(embedded.Brokers)

	// User copy present -> it wins over embedded.
	userPath := UserBrokersPath()
	if err := os.MkdirAll(filepath.Dir(userPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(userPath, []byte("brokers:\n  - {id: only, name: Only, email: a@b.c, region: us}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Brokers) != 1 || got.Brokers[0].ID != "only" {
		t.Errorf("user copy did not win: %+v", got.Brokers)
	}

	// Explicit override wins over both.
	overridePath := filepath.Join(tmp, "override.yaml")
	if err := os.WriteFile(overridePath, []byte("brokers:\n  - {id: ov1, name: O1, email: x@y.z, region: eu}\n  - {id: ov2, name: O2, email: p@q.r, region: eu}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err = Load(overridePath)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Brokers) != 2 {
		t.Errorf("override did not win: got %d entries", len(got.Brokers))
	}

	if embeddedN < 200 {
		t.Errorf("sanity: embedded count %d too low", embeddedN)
	}
}

func TestLoadList(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp) // isolate from any real ~/.eraser/brokers.yaml

	full, err := LoadList("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	verified, err := LoadList("", "", "verified")
	if err != nil {
		t.Fatal(err)
	}
	if len(verified.Brokers) == 0 || len(verified.Brokers) >= len(full.Brokers) {
		t.Fatalf("verified list should be non-empty and smaller than full: verified=%d full=%d", len(verified.Brokers), len(full.Brokers))
	}

	// configPath (options.broker_file) beats the verified list.
	cfgList := filepath.Join(tmp, "cfg.yaml")
	if err := os.WriteFile(cfgList, []byte("brokers:\n  - {id: c1, name: C1, email: a@b.c, region: us}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := LoadList("", cfgList, "verified")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Brokers) != 1 || got.Brokers[0].ID != "c1" {
		t.Errorf("broker_file did not win over verified: %+v", got.Brokers)
	}

	// overridePath (--brokers) beats everything.
	flagList := filepath.Join(tmp, "flag.yaml")
	if err := os.WriteFile(flagList, []byte("brokers:\n  - {id: f1, name: F1, email: a@b.c, region: us}\n  - {id: f2, name: F2, email: d@e.f, region: us}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err = LoadList(flagList, cfgList, "verified")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Brokers) != 2 {
		t.Errorf("--brokers did not win: got %d entries", len(got.Brokers))
	}
}
