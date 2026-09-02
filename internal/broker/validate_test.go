package broker

import (
	"fmt"
	"strings"
	"testing"

	"github.com/drumandbytes/eraser/data"
)

// TestEmbeddedBrokerDataValid is the CI guard: the broker list shipped in the
// binary must always pass structural validation.
func TestEmbeddedBrokerDataValid(t *testing.T) {
	if err := Validate(data.BrokersYAML); err != nil {
		t.Fatalf("embedded data/brokers.yaml is invalid:\n%v", err)
	}
}

func TestValidateCatchesProblems(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		want string
	}{
		{"truncated", "brokers:\n  - id: a\n    name: A\n    region: us\n", "truncated"},
		{"duplicate id", fillerYAML("  - id: filler-0\n    name: Dup\n    region: us\n"), "duplicate id"},
		{"missing name", fillerYAML("  - id: lonely\n    region: us\n"), "missing name"},
		{"missing id", fillerYAML("  - name: No ID\n    region: us\n"), "missing id"},
		{"unknown region", fillerYAML("  - id: weird\n    name: Weird\n    region: usa\n"), "unknown region"},
		{"implausible email", fillerYAML("  - id: bade\n    name: Bad Email\n    region: us\n    email: not-an-email\n"), "implausible email"},
		{"malformed opt_out_url", fillerYAML("  - id: badurl\n    name: Bad URL\n    region: us\n    opt_out_url: \"javascript:alert(1)\"\n"), "opt_out_url"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := Validate([]byte(tc.yaml))
			if err == nil {
				t.Fatalf("expected an error mentioning %q, got nil", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not mention %q", err.Error(), tc.want)
			}
		})
	}
}

func TestValidateAcceptsGoodData(t *testing.T) {
	if err := Validate([]byte(fillerYAML(""))); err != nil {
		t.Fatalf("expected valid, got: %v", err)
	}
}

// fillerYAML returns a brokers document with MinSaneBrokerCount valid entries
// plus whatever extra entry lines the caller appends.
func fillerYAML(extra string) string {
	var b strings.Builder
	b.WriteString("brokers:\n")
	for i := 0; i < MinSaneBrokerCount; i++ {
		fmt.Fprintf(&b, "  - id: filler-%d\n    name: Filler %d\n    region: us\n    email: privacy@filler%d.example\n", i, i, i)
	}
	b.WriteString(extra)
	return b.String()
}
