package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const profile = `mode: set
github.com/drumandbytes/eraser/internal/a/a.go:10.1,12.2 3 1
github.com/drumandbytes/eraser/internal/a/a.go:14.1,16.2 2 0
github.com/drumandbytes/eraser/scripts/tool/main.go:1.1,9.2 50 0
github.com/drumandbytes/eraser/internal/imaptest/x.go:1.1,9.2 50 0
`

func TestParseProfileDropsIgnoredFiles(t *testing.T) {
	blocks, err := parseProfile(strings.NewReader(profile), []string{"scripts/", "internal/imaptest/"})
	if err != nil {
		t.Fatal(err)
	}
	if len(blocks) != 2 || blocks[0].file != "internal/a/a.go" {
		t.Fatalf("blocks = %+v", blocks)
	}
	covered, all := total(blocks)
	if covered != 3 || all != 5 {
		t.Fatalf("total = %d/%d, want 3/5", covered, all)
	}
	for _, bad := range []string{"nocolon", "f.go:1.1,2.2 x 1", "f.go:1.1,2.2 1"} {
		if _, err := parseProfile(strings.NewReader("mode: set\n"+bad+"\n"), nil); err == nil {
			t.Errorf("accepted bad line %q", bad)
		}
	}
}

func TestChangedLinesAndPatch(t *testing.T) {
	diff := `diff --git a/internal/a/a.go b/internal/a/a.go
--- a/internal/a/a.go
+++ b/internal/a/a.go
@@ -9,0 +10,2 @@ func x() {
+added
+added
@@ -20 +22 @@
+one line
diff --git a/old.go b/old.go
--- a/old.go
+++ /dev/null
@@ -1,3 +0,0 @@
-gone
`
	changed := changedLines(diff)
	if !reflect.DeepEqual(changed["internal/a/a.go"], map[int]bool{10: true, 11: true, 22: true}) || changed["old.go"] != nil {
		t.Fatalf("changed = %v", changed)
	}
	blocks, _ := parseProfile(strings.NewReader(profile), []string{"scripts/", "internal/imaptest/"})
	covered, all := patch(blocks, changed)
	if covered != 3 || all != 3 {
		t.Fatalf("patch = %d/%d, want 3/3 (only the first block is touched)", covered, all)
	}
	// Touch the uncovered block too.
	changed["internal/a/a.go"][15] = true
	if covered, all = patch(blocks, changed); covered != 3 || all != 5 {
		t.Fatalf("patch = %d/%d, want 3/5", covered, all)
	}
}

func TestCheck(t *testing.T) {
	l := limits{projectMin: 88, patchMin: 80, patchMinStatements: 10}
	if fails := check(l, 90, 100, 8, 10); len(fails) != 0 {
		t.Fatalf("passing run failed: %v", fails)
	}
	if fails := check(l, 80, 100, 8, 10); len(fails) != 1 || !strings.Contains(fails[0], "project") {
		t.Fatalf("low project: %v", fails)
	}
	if fails := check(l, 90, 100, 7, 10); len(fails) != 1 || !strings.Contains(fails[0], "patch") {
		t.Fatalf("low patch: %v", fails)
	}
	if fails := check(l, 90, 100, 0, 9); len(fails) != 0 {
		t.Fatalf("a tiny patch should not be gated: %v", fails)
	}
	if fails := check(l, 70, 100, 0, 20); len(fails) != 2 {
		t.Fatalf("both limits should fail: %v", fails)
	}
	if pct(0, 0) != 100 {
		t.Fatal("an empty patch is fully covered")
	}
}

func TestIgnoreFromCodecov(t *testing.T) {
	path := filepath.Join(t.TempDir(), "codecov.yml")
	if err := os.WriteFile(path, []byte("ignore:\n  - \"scripts/**\"\n  - \"internal/imaptest/**\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := ignoreFromCodecov(path)
	if err != nil || !reflect.DeepEqual(got, []string{"scripts/", "internal/imaptest/"}) {
		t.Fatalf("ignore = %v, %v", got, err)
	}
	if _, err := ignoreFromCodecov(filepath.Join(t.TempDir(), "missing.yml")); err == nil {
		t.Fatal("missing file accepted")
	}
}
