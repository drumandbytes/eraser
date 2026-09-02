// Command import-registries helps grow data/brokers.yaml from public
// data-broker lists: the US state registries (California CPPA, Vermont, Oregon,
// Texas) and the IAB TCF Global Vendor List.
//
// The US state registries are one-click CSV/XLSX downloads behind state
// websites, not stable APIs, so fetching those is a manual step - see
// docs/auditing.md for the URLs. The IAB GVL is a public JSON the tool fetches
// itself. Either way this tool does the tedious half: it normalises the rows,
// diffs them against the list already embedded in eraser, and writes two files:
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
//
//	go run ./scripts/import-registries -gvl https://vendor-list.consensu.org/v3/vendor-list.json
//
// Note: the GVL is ad-tech vendors, most of them cookie/RTB-based. It yields
// hundreds of candidates with no opt-out email and is best used as a periodic
// cross-check for missing big names, not a bulk import - see docs/auditing.md.
package main

import (
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/eraser-privacy/eraser/internal/broker"
	"gopkg.in/yaml.v3"
)

func main() {
	csvPath := flag.String("csv", "", "path to a registry CSV export")
	nameCol := flag.String("name-col", "", "header of the broker-name column (required with -csv)")
	urlCol := flag.String("url-col", "", "header of the website/URL column (optional)")
	emailCol := flag.String("email-col", "", "header of the email column (optional)")
	gvl := flag.String("gvl", "", "path or URL to the IAB TCF Global Vendor List JSON (e.g. https://vendor-list.consensu.org/v3/vendor-list.json)")
	gvlAll := flag.Bool("gvl-all", false, "with -gvl, include every active vendor, not just those declaring identifiable data (profiles, user-provided data, auth identifiers)")
	region := flag.String("region", "", "region to stamp on new entries (default: us for -csv, global for -gvl)")
	note := flag.String("note", "", "override the Notes string on new entries")
	outDir := flag.String("out", ".", "directory for candidates.yaml + review.md")
	flag.Parse()

	var (
		rows        []row
		err         error
		defRegion   = "us"
		defNote     = "From a state data-broker registry; verify opt-out email/URL before use."
		sourceLabel = "registry"
	)

	switch {
	case *gvl != "":
		rows, err = readGVL(*gvl, *gvlAll)
		defRegion = "global"
		defNote = "IAB TCF ad-tech vendor; cookie/device-ID based, so a name-based erasure request may not match anything - check the vendor's DSR/privacy portal for the right route."
		sourceLabel = "IAB TCF Global Vendor List"
	case *csvPath != "":
		if *nameCol == "" {
			fatal(fmt.Errorf("-name-col is required with -csv"))
		}
		rows, err = readCSV(*csvPath, *nameCol, *urlCol, *emailCol)
	default:
		flag.Usage()
		os.Exit(2)
	}
	if err != nil {
		fatal(err)
	}
	if *region != "" {
		defRegion = *region
	}
	if *note != "" {
		defNote = *note
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

		notes := defNote
		if row.note != "" {
			notes = row.note + " " + defNote
		}
		candidates = append(candidates, broker.Broker{
			ID:       slug(row.name),
			Name:     row.name,
			Email:    row.email,
			Website:  row.url,
			Region:   defRegion,
			Category: "", // fill in: people-search / marketing / background-check / ...
			Notes:    notes,
		})
	}

	sort.Slice(candidates, func(i, j int) bool { return candidates[i].ID < candidates[j].ID })
	sort.Strings(review)

	writeCandidates(filepath.Join(*outDir, "candidates.yaml"), candidates)
	writeReview(filepath.Join(*outDir, "review.md"), review, len(rows), len(candidates))

	fmt.Printf("%d rows from %s -> %d candidates, %d near-matches to review\n", len(rows), sourceLabel, len(candidates), len(review))
	fmt.Printf("wrote %s and %s\n", filepath.Join(*outDir, "candidates.yaml"), filepath.Join(*outDir, "review.md"))
}

type row struct{ name, url, email, note string }

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

// gvl is the subset of the IAB TCF Global Vendor List JSON we care about.
type gvl struct {
	VendorListVersion int `json:"vendorListVersion"`
	Vendors           map[string]struct {
		ID              int    `json:"id"`
		Name            string `json:"name"`
		Purposes        []int  `json:"purposes"`
		LegIntPurposes  []int  `json:"legIntPurposes"`
		DataDeclaration []int  `json:"dataDeclaration"`
		DeletedDate     string `json:"deletedDate"`
		URLs            []struct {
			LangID  string `json:"langId"`
			Privacy string `json:"privacy"`
		} `json:"urls"`
	} `json:"vendors"`
}

// TCF data categories that mean the vendor can plausibly tie data to an
// identifiable person (so a GDPR erasure request has something to act on):
// 5 auth-derived identifiers, 7 user-provided data, 10 users' profiles.
var identifiableDataCats = map[int]bool{5: true, 7: true, 10: true}

func readGVL(src string, includeAll bool) ([]row, error) {
	var raw []byte
	var err error
	if strings.HasPrefix(src, "http://") || strings.HasPrefix(src, "https://") {
		client := &http.Client{Timeout: 30 * time.Second}
		req, _ := http.NewRequest(http.MethodGet, src, nil)
		req.Header.Set("User-Agent", "eraser-import-registries")
		resp, e := client.Do(req)
		if e != nil {
			return nil, fmt.Errorf("fetching GVL: %w", e)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("fetching GVL: %s", resp.Status)
		}
		raw, err = io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	} else {
		raw, err = os.ReadFile(src)
	}
	if err != nil {
		return nil, err
	}

	var g gvl
	if err := json.Unmarshal(raw, &g); err != nil {
		return nil, fmt.Errorf("parsing GVL JSON: %w", err)
	}

	var out []row
	for _, v := range g.Vendors {
		if v.DeletedDate != "" || v.Name == "" {
			continue
		}
		// Only vendors that build or use advertising profiles.
		if !containsAny(v.Purposes, 3, 4) && !containsAny(v.LegIntPurposes, 3, 4) {
			continue
		}
		if !includeAll {
			ident := false
			for _, c := range v.DataDeclaration {
				if identifiableDataCats[c] {
					ident = true
					break
				}
			}
			if !ident {
				continue
			}
		}
		privacy := ""
		for _, u := range v.URLs {
			if u.LangID == "en" || privacy == "" {
				privacy = u.Privacy
			}
		}
		out = append(out, row{
			name: v.Name,
			url:  normURL(privacy),
			note: fmt.Sprintf("IAB TCF vendor #%d (GVL v%d).", v.ID, g.VendorListVersion),
		})
	}
	return out, nil
}

func containsAny(haystack []int, needles ...int) bool {
	for _, h := range haystack {
		for _, n := range needles {
			if h == n {
				return true
			}
		}
	}
	return false
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
