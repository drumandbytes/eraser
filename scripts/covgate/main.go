// covgate fails CI when coverage drops: the project total under a floor, or too
// little of the lines a PR changed covered. It enforces what codecov.yml asks
// for without needing Codecov's GitHub app, which posts no commit statuses here.
//
//	go test -coverprofile=coverage.out ./...
//	go run ./scripts/covgate -profile coverage.out -base origin/main
package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

const module = "github.com/drumandbytes/eraser/"

type block struct {
	file       string
	start, end int
	stmts      int
	covered    bool
}

// parseProfile reads a Go cover profile, dropping files under an ignored prefix.
func parseProfile(r io.Reader, ignore []string) ([]block, error) {
	var out []block
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "mode:") || line == "" {
			continue
		}
		// file.go:12.3,15.2 4 1
		colon := strings.LastIndex(line, ":")
		fields := strings.Fields(line[colon+1:])
		if colon < 0 || len(fields) != 3 {
			return nil, fmt.Errorf("bad profile line %q", line)
		}
		file := strings.TrimPrefix(line[:colon], module)
		if ignored(file, ignore) {
			continue
		}
		pos := strings.Split(fields[0], ",")
		start, err1 := strconv.Atoi(strings.SplitN(pos[0], ".", 2)[0])
		end, err2 := strconv.Atoi(strings.SplitN(pos[1], ".", 2)[0])
		stmts, err3 := strconv.Atoi(fields[1])
		count, err4 := strconv.Atoi(fields[2])
		if err1 != nil || err2 != nil || err3 != nil || err4 != nil {
			return nil, fmt.Errorf("bad profile line %q", line)
		}
		out = append(out, block{file, start, end, stmts, count > 0})
	}
	return out, sc.Err()
}

func ignored(file string, ignore []string) bool {
	for _, p := range ignore {
		if strings.HasPrefix(file, p) {
			return true
		}
	}
	return false
}

// total returns covered and total statements.
func total(blocks []block) (covered, all int) {
	for _, b := range blocks {
		all += b.stmts
		if b.covered {
			covered += b.stmts
		}
	}
	return
}

var hunkRe = regexp.MustCompile(`^@@ -\S+ \+(\d+)(?:,(\d+))? @@`)

// changedLines reads `git diff -U0` output into file -> added line numbers.
func changedLines(diff string) map[string]map[int]bool {
	out := map[string]map[int]bool{}
	var file string
	for _, line := range strings.Split(diff, "\n") {
		switch {
		case strings.HasPrefix(line, "+++ b/"):
			file = strings.TrimPrefix(line, "+++ b/")
		case strings.HasPrefix(line, "+++ "):
			file = "" // /dev/null: a deleted file
		case strings.HasPrefix(line, "@@") && file != "":
			m := hunkRe.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			start, _ := strconv.Atoi(m[1])
			n := 1
			if m[2] != "" {
				n, _ = strconv.Atoi(m[2])
			}
			if out[file] == nil {
				out[file] = map[int]bool{}
			}
			for i := 0; i < n; i++ {
				out[file][start+i] = true
			}
		}
	}
	return out
}

// patch counts the statements in blocks that touch a changed line.
func patch(blocks []block, changed map[string]map[int]bool) (covered, all int) {
	for _, b := range blocks {
		lines := changed[b.file]
		for l := b.start; l <= b.end && lines != nil; l++ {
			if lines[l] {
				all += b.stmts
				if b.covered {
					covered += b.stmts
				}
				break
			}
		}
	}
	return
}

func pct(covered, all int) float64 {
	if all == 0 {
		return 100
	}
	return 100 * float64(covered) / float64(all)
}

type limits struct {
	projectMin, patchMin float64
	patchMinStatements   int
}

// check returns the reasons the gate fails, one per line; empty means it passes.
func check(l limits, projCov, projAll, patchCov, patchAll int) []string {
	var fails []string
	if p := pct(projCov, projAll); p < l.projectMin {
		fails = append(fails, fmt.Sprintf("project coverage %.1f%% is below the %.1f%% floor", p, l.projectMin))
	}
	// Tiny patches swing wildly, so they are reported but not gated.
	if patchAll >= l.patchMinStatements {
		if p := pct(patchCov, patchAll); p < l.patchMin {
			fails = append(fails, fmt.Sprintf("patch coverage %.1f%% is below the %.1f%% target (%d of %d changed statements)", p, l.patchMin, patchCov, patchAll))
		}
	}
	return fails
}

// ignoreFromCodecov reads codecov.yml's ignore globs as path prefixes.
func ignoreFromCodecov(path string) ([]string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg struct {
		Ignore []string `yaml:"ignore"`
	}
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return nil, err
	}
	var out []string
	for _, g := range cfg.Ignore {
		out = append(out, strings.TrimRight(strings.TrimSuffix(g, "**"), "*"))
	}
	return out, nil
}

func main() {
	profile := flag.String("profile", "coverage.out", "Go cover profile")
	base := flag.String("base", "origin/main", "git revision the PR is compared with")
	codecov := flag.String("codecov", "codecov.yml", "file whose ignore list to honour")
	projectMin := flag.Float64("project-min", 88, "minimum total coverage, percent")
	patchMin := flag.Float64("patch-min", 80, "minimum coverage of changed lines, percent")
	patchStmts := flag.Int("patch-min-statements", 10, "smaller patches are reported, not gated")
	flag.Parse()

	ignore, err := ignoreFromCodecov(*codecov)
	if err != nil {
		log.Fatal(err)
	}
	f, err := os.Open(*profile)
	if err != nil {
		log.Fatal(err)
	}
	blocks, err := parseProfile(f, ignore)
	_ = f.Close()
	if err != nil {
		log.Fatal(err)
	}
	diff, err := exec.Command("git", "diff", "-U0", *base+"...HEAD", "--", "*.go", ":!*_test.go").Output()
	if err != nil {
		log.Fatalf("git diff against %s: %v", *base, err)
	}
	projCov, projAll := total(blocks)
	patchCov, patchAll := patch(blocks, changedLines(string(diff)))
	fails := check(limits{*projectMin, *patchMin, *patchStmts}, projCov, projAll, patchCov, patchAll)

	summary := fmt.Sprintf("Project coverage: %.1f%% (floor %.1f%%)\nPatch coverage: %.1f%% of %d changed statements (target %.1f%%)\n",
		pct(projCov, projAll), *projectMin, pct(patchCov, patchAll), patchAll, *patchMin)
	fmt.Print(summary)
	if path := os.Getenv("GITHUB_STEP_SUMMARY"); path != "" {
		if f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644); err == nil {
			_, _ = fmt.Fprintf(f, "### Coverage gate\n\n```\n%s```\n", summary)
			_ = f.Close()
		}
	}
	for _, msg := range fails {
		fmt.Println("FAIL:", msg)
	}
	if len(fails) > 0 {
		os.Exit(1)
	}
}
