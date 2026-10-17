// docs-versions writes the versioned documentation pages for the site.
//
// Every release line gets a snapshot of the user-facing docs taken from its
// newest tag, plus a "next" snapshot of the working tree and, when a prerelease
// is newer than the last stable release, a beta snapshot. Hugo then builds all
// of them in one pass with the same layout.
//
//	go run ./scripts/docs-versions            # writes site/content/docs/*, site/data/docs_versions.json
package main

import (
	"cmp"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// pages are the user-facing docs. Developer notes stay out of the site.
var pages = []string{"commands", "multi-profile", "mcp", "stability", "broker-replies"}

const repoURL = "https://github.com/drumandbytes/eraser"

type Version struct {
	Slug   string   `json:"slug"`
	Label  string   `json:"label"`
	Status string   `json:"status"` // nightly, beta or stable
	Latest bool     `json:"latest,omitempty"`
	Pages  []string `json:"pages"`
	ref    string   // git tag, or "" for the working tree
}

type semver struct {
	major, minor, patch int
	pre                 []string
}

var tagRe = regexp.MustCompile(`^v(\d+)\.(\d+)\.(\d+)(?:-([0-9A-Za-z.-]+))?$`)

func parseTag(tag string) (semver, bool) {
	m := tagRe.FindStringSubmatch(tag)
	if m == nil {
		return semver{}, false
	}
	v := semver{}
	v.major, _ = strconv.Atoi(m[1])
	v.minor, _ = strconv.Atoi(m[2])
	v.patch, _ = strconv.Atoi(m[3])
	if m[4] != "" {
		v.pre = strings.Split(m[4], ".")
	}
	return v, true
}

// compare orders versions by semver precedence: 2.0.0-beta.2 < 2.0.0-beta.10 < 2.0.0.
func (a semver) compare(b semver) int {
	for _, c := range []int{cmp.Compare(a.major, b.major), cmp.Compare(a.minor, b.minor), cmp.Compare(a.patch, b.patch)} {
		if c != 0 {
			return c
		}
	}
	switch {
	case len(a.pre) == 0 && len(b.pre) == 0:
		return 0
	case len(a.pre) == 0:
		return 1
	case len(b.pre) == 0:
		return -1
	}
	for i := 0; i < len(a.pre) && i < len(b.pre); i++ {
		x, xerr := strconv.Atoi(a.pre[i])
		y, yerr := strconv.Atoi(b.pre[i])
		var c int
		switch {
		case xerr == nil && yerr == nil:
			c = cmp.Compare(x, y)
		case xerr == nil:
			c = -1 // numeric identifiers sort before text
		case yerr == nil:
			c = 1
		default:
			c = strings.Compare(a.pre[i], b.pre[i])
		}
		if c != 0 {
			return c
		}
	}
	return cmp.Compare(len(a.pre), len(b.pre))
}

// selectVersions returns the dropdown entries: next, an optional beta, then the
// newest tag of every stable minor line, newest first.
func selectVersions(tags []string) []Version {
	type tagged struct {
		tag string
		v   semver
	}
	stable := map[[2]int]tagged{}
	var top tagged
	var beta *tagged
	for _, tag := range tags {
		// 0.x predates the 1.x stability promise; its docs are not worth a version.
		v, ok := parseTag(tag)
		if !ok || v.major == 0 {
			continue
		}
		t := tagged{tag, v}
		if len(v.pre) > 0 {
			if beta == nil || v.compare(beta.v) > 0 {
				beta = &t
			}
			continue
		}
		key := [2]int{v.major, v.minor}
		if cur, ok := stable[key]; !ok || v.compare(cur.v) > 0 {
			stable[key] = t
		}
		if top.tag == "" || v.compare(top.v) > 0 {
			top = t
		}
	}

	out := []Version{{Slug: "next", Label: "Nightly", Status: "nightly"}}
	// A beta only matters while it is ahead of the newest stable release.
	if beta != nil && (top.tag == "" || beta.v.compare(top.v) > 0) {
		out = append(out, Version{Slug: fmt.Sprintf("%d.%d-beta", beta.v.major, beta.v.minor), Label: fmt.Sprintf("%d.%d beta", beta.v.major, beta.v.minor), Status: "beta", ref: beta.tag})
	}
	var lines []tagged
	for _, t := range stable {
		lines = append(lines, t)
	}
	slices.SortFunc(lines, func(a, b tagged) int { return b.v.compare(a.v) })
	for i, t := range lines {
		slug := fmt.Sprintf("%d.%d", t.v.major, t.v.minor)
		out = append(out, Version{Slug: slug, Label: slug, Status: "stable", Latest: i == 0, ref: t.tag})
	}
	return out
}

var (
	docLink    = regexp.MustCompile(`\]\(([A-Za-z0-9_-]+)\.md(#[^)]*)?\)`)
	parentLink = regexp.MustCompile(`\]\(\.\./([A-Za-z0-9_.-]+)(#[^)]*)?\)`)
)

// render turns one markdown doc into a Hugo page. The first heading becomes the
// page title, and relative links are pointed at the site or at GitHub.
func render(name, body string, v Version) string {
	lines := strings.Split(body, "\n")
	title := name
	for i, l := range lines {
		if t, ok := strings.CutPrefix(l, "# "); ok {
			title = strings.TrimSpace(t)
			lines = append(lines[:i], lines[i+1:]...)
			break
		}
	}
	text := strings.Join(lines, "\n")
	ref := "main"
	if v.ref != "" {
		ref = v.ref
	}
	known := map[string]bool{}
	for _, p := range pages {
		known[p] = true
	}
	text = docLink.ReplaceAllStringFunc(text, func(m string) string {
		sub := docLink.FindStringSubmatch(m)
		if known[sub[1]] {
			return "](../" + sub[1] + "/" + sub[2] + ")"
		}
		return "](" + repoURL + "/blob/" + ref + "/docs/" + sub[1] + ".md" + sub[2] + ")"
	})
	text = parentLink.ReplaceAllString(text, "]("+repoURL+"/blob/"+ref+"/$1$2)")

	var fm strings.Builder
	fm.WriteString("---\n")
	fmt.Fprintf(&fm, "title: %s\n", strconv.Quote(title))
	fmt.Fprintf(&fm, "docs_version: %s\ndocs_page: %s\n", strconv.Quote(v.Slug), strconv.Quote(name))
	if !v.Latest {
		fm.WriteString("robots: noindex\nsitemap:\n  disable: true\n")
	}
	fm.WriteString("---\n\n")
	return fm.String() + strings.TrimLeft(text, "\n")
}

func git(repo string, args ...string) ([]byte, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = repo
	return cmd.Output()
}

func readDoc(repo string, v Version, page string) ([]byte, bool) {
	if v.ref == "" {
		b, err := os.ReadFile(filepath.Join(repo, "docs", page+".md"))
		return b, err == nil
	}
	b, err := git(repo, "show", v.ref+":docs/"+page+".md")
	return b, err == nil
}

func run(repo, siteDir string) error {
	raw, err := git(repo, "tag", "--list", "v*")
	if err != nil {
		return fmt.Errorf("list tags: %w", err)
	}
	versions := selectVersions(strings.Fields(string(raw)))

	content := filepath.Join(siteDir, "content", "docs")
	entries, _ := os.ReadDir(content)
	for _, e := range entries { // drop stale snapshots; _index.md stays
		if e.IsDir() {
			if err := os.RemoveAll(filepath.Join(content, e.Name())); err != nil {
				return err
			}
		}
	}

	for i := range versions {
		v := &versions[i]
		v.Pages = []string{}
		dir := filepath.Join(content, v.Slug)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		for _, page := range pages {
			body, ok := readDoc(repo, *v, page)
			if !ok {
				continue
			}
			if err := os.WriteFile(filepath.Join(dir, page+".md"), []byte(render(page, string(body), *v)), 0o644); err != nil {
				return err
			}
			v.Pages = append(v.Pages, page)
		}
		index := fmt.Sprintf("---\ntitle: %s\ndocs_version: %s\ndocs_status: %s\n", strconv.Quote("Eraser "+v.Label+" documentation"), strconv.Quote(v.Slug), strconv.Quote(v.Status))
		if !v.Latest {
			index += "robots: noindex\nsitemap:\n  disable: true\n"
		}
		if err := os.WriteFile(filepath.Join(dir, "_index.md"), []byte(index+"---\n"), 0o644); err != nil {
			return err
		}
	}

	data, err := json.MarshalIndent(map[string]any{"versions": versions}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(siteDir, "data"), 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(siteDir, "data", "docs_versions.json"), append(data, '\n'), 0o644)
}

func main() {
	repo := flag.String("repo", ".", "repository root")
	site := flag.String("site", "site", "Hugo site directory")
	flag.Parse()
	if err := run(*repo, filepath.Join(*repo, *site)); err != nil {
		log.Fatal(err)
	}
}
