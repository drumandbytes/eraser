// Command import-registries helps grow data/brokers.yaml from the public US
// state data-broker registries (California CPPA, Vermont, Oregon, Texas).
//
// The registries are one-click CSV/XLSX downloads behind state websites, not
// stable APIs, so fetching is a manual step - see docs/auditing.md for the
// URLs. This tool does the tedious half: given a registry CSV, it normalises
// the rows, diffs them against the list already embedded in eraser, and writes
// two files:
//
//	candidates.yaml - rows with no obvious match in the current list, in
//	                  brokers.yaml shape, ready for you to add email/opt_out_url
//	                  and paste into data/brokers.yaml
//	review.md       - rows that fuzzy-match an existing broker (probably already
//	                  covered; skim to be sure)
//
// Usage:
//
//	go run ./scripts/import-registries -csv registry.csv -name-col "Data Broker Name" \
//	    [-url-col "Website"] [-email-col "Email Address"] [-out .]
package main

import (
	"encoding/csv"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/drumandbytes/eraser/internal/broker"
	"gopkg.in/yaml.v3"
)

func main() {
	csvPath := flag.String("csv", "", "path to a registry CSV export (required)")
	nameCol := flag.String("name-col", "", "header of the broker-name column (required)")
	urlCol := flag.String("url-col", "", "header of the website/URL column (optional)")
	emailCol := flag.String("email-col", "", "header of the email column (optional)")
	region := flag.String("region", "us", "region to stamp on new entries")
	outDir := flag.String("out", ".", "directory for candidates.yaml + review.md")
	flag.Parse()

	if *csvPath == "" || *nameCol == "" {
		flag.Usage()
		os.Exit(2)
	}

	rows, err := readCSV(*csvPath, *nameCol, *urlCol, *emailCol)
	if err != nil {
		fatal(err)
	}

	existing, err := broker.Load("")
	if err != nil {
		fatal(fmt.Errorf("loading embedded broker list: %w", err))
	}
	byName := map[string]broker.Broker{}
	byDomain := map[string]broker.Broker{}
	for _, b := range existing.Brokers {
		byName[normName(b.Name)] = b
		if d := domain(b.Website); d != "" {
			byDomain[d] = b
		}
		if d := domainFromEmail(b.Email); d != "" {
			byDomain[d] = b
		}
	}

	var candidates []broker.Broker
	var review []string
	seen := map[string]bool{}

	for _, row := range rows {
		key := normName(row.name)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true

		if m, ok := byName[key]; ok {
			review = append(review, fmt.Sprintf("- **%s** - name matches existing `%s`", row.name, m.ID))
			continue
		}
		if d := domain(row.url); d != "" {
			if m, ok := byDomain[d]; ok {
				review = append(review, fmt.Sprintf("- **%s** (%s) - domain matches existing `%s`", row.name, d, m.ID))
				continue
			}
		}

		candidates = append(candidates, broker.Broker{
			ID:       slug(row.name),
			Name:     row.name,
			Email:    row.email,
			Website:  row.url,
			Region:   *region,
			Category: "", // fill in: people-search / marketing / background-check / ...
			Notes:    "From state data-broker registry; verify opt-out email/URL before use.",
		})
	}

	sort.Slice(candidates, func(i, j int) bool { return candidates[i].ID < candidates[j].ID })
	sort.Strings(review)

	writeCandidates(filepath.Join(*outDir, "candidates.yaml"), candidates)
	writeReview(filepath.Join(*outDir, "review.md"), review, len(rows), len(candidates))

	fmt.Printf("%d registry rows -> %d candidates, %d near-matches to review\n", len(rows), len(candidates), len(review))
	fmt.Printf("wrote %s and %s\n", filepath.Join(*outDir, "candidates.yaml"), filepath.Join(*outDir, "review.md"))
}

type row struct{ name, url, email string }

func readCSV(path, nameCol, urlCol, emailCol string) ([]row, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	r := csv.NewReader(f)
	r.FieldsPerRecord = -1
	header, err := r.Read()
	if err != nil {
		return nil, fmt.Errorf("reading header: %w", err)
	}
	idx := func(name string) int {
		for i, h := range header {
			if strings.EqualFold(strings.TrimSpace(h), strings.TrimSpace(name)) {
				return i
			}
		}
		return -1
	}
	ni, ui, ei := idx(nameCol), idx(urlCol), idx(emailCol)
	if ni < 0 {
		return nil, fmt.Errorf("no column %q in header %v", nameCol, header)
	}

	var out []row
	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		get := func(i int) string {
			if i >= 0 && i < len(rec) {
				return strings.TrimSpace(rec[i])
			}
			return ""
		}
		name := get(ni)
		if name == "" {
			continue
		}
		out = append(out, row{name: name, url: normURL(get(ui)), email: strings.ToLower(get(ei))})
	}
	return out, nil
}

var nonAlnum = regexp.MustCompile(`[^a-z0-9]+`)

func normName(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	for _, suffix := range []string{" inc", " inc.", " llc", " l.l.c.", " ltd", " ltd.", " corp", " corp.", " co", " co.", ", inc", ", llc"} {
		s = strings.TrimSuffix(s, suffix)
	}
	return strings.TrimSpace(nonAlnum.ReplaceAllString(s, " "))
}

func slug(s string) string {
	return strings.Trim(nonAlnum.ReplaceAllString(strings.ToLower(s), "-"), "-")
}

func normURL(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if !strings.HasPrefix(s, "http://") && !strings.HasPrefix(s, "https://") {
		s = "https://" + s
	}
	return s
}

func domain(rawURL string) string {
	s := strings.TrimPrefix(strings.TrimPrefix(rawURL, "https://"), "http://")
	s = strings.TrimPrefix(s, "www.")
	if i := strings.IndexAny(s, "/?#"); i >= 0 {
		s = s[:i]
	}
	return strings.ToLower(s)
}

func domainFromEmail(email string) string {
	if i := strings.LastIndex(email, "@"); i >= 0 {
		return strings.ToLower(email[i+1:])
	}
	return ""
}

func writeCandidates(path string, brokers []broker.Broker) {
	var b strings.Builder
	b.WriteString("# Candidates from a state data-broker registry.\n")
	b.WriteString("# Fill in `category` and a working `email` or `opt_out_url` for each,\n")
	b.WriteString("# then move the entries into data/brokers.yaml. Drop any that don't\n")
	b.WriteString("# actually hold consumer profile data.\n")
	enc := yaml.NewEncoder(&b)
	enc.SetIndent(2)
	if err := enc.Encode(broker.BrokerDatabase{Brokers: brokers}); err != nil {
		fatal(err)
	}
	_ = enc.Close()
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		fatal(err)
	}
}

func writeReview(path string, lines []string, total, candidates int) {
	var b strings.Builder
	fmt.Fprintf(&b, "# Registry import - review\n\n%d rows in, %d new candidates, %d near-matches below.\n\n", total, candidates, len(lines))
	b.WriteString("These registry entries fuzzy-match a broker already in the list - probably\nalready covered, but skim in case the match is wrong.\n\n")
	for _, l := range lines {
		b.WriteString(l)
		b.WriteString("\n")
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		fatal(err)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "import-registries:", err)
	os.Exit(1)
}
