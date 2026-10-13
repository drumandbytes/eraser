package broker

import (
	"flag"
	"os"
	"testing"

	"github.com/drumandbytes/eraser/data"
	"gopkg.in/yaml.v3"
)

var updateCanonical = flag.Bool("update", false, "rewrite data/*.yaml in canonical form")

// The audit's --fix and the bounce tooling save through yaml.Marshal, so any
// hand-edited file that isn't already in that form gets reformatted in full on
// the next weekly audit PR. Keep the shipped lists canonical so those diffs
// only contain real changes. Normalize with:
//
//	go test ./internal/broker -run TestEmbeddedListsCanonical -update
func TestEmbeddedListsCanonical(t *testing.T) {
	for _, f := range []struct {
		path string
		raw  []byte
	}{
		{"../../data/brokers.yaml", data.BrokersYAML},
		{"../../data/brokers-verified.yaml", data.BrokersVerifiedYAML},
	} {
		var db BrokerDatabase
		if err := yaml.Unmarshal(f.raw, &db); err != nil {
			t.Fatalf("%s: %v", f.path, err)
		}
		want, err := yaml.Marshal(&db)
		if err != nil {
			t.Fatalf("%s: %v", f.path, err)
		}
		if string(want) == string(f.raw) {
			continue
		}
		if *updateCanonical {
			if err := os.WriteFile(f.path, want, 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		t.Errorf("%s is not in canonical YAML form; run: go test ./internal/broker -run TestEmbeddedListsCanonical -update", f.path)
	}
}
