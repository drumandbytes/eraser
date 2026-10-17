package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func slugs(vs []Version) []string {
	var out []string
	for _, v := range vs {
		out = append(out, v.Slug)
	}
	return out
}

func TestSelectVersions(t *testing.T) {
	tags := []string{"v1.0.0", "v1.1.0", "v1.2.8", "v1.2.9", "v1.2.10", "v2.0.0-beta.2", "v2.0.0-beta.10", "v2.0.0-beta.1", "latest", "v1", "v0.9.0"}
	got := selectVersions(tags)
	if want := []string{"next", "2.0-beta", "1.2", "1.1", "1.0"}; !reflect.DeepEqual(slugs(got), want) {
		t.Fatalf("slugs = %v, want %v", slugs(got), want)
	}
	if got[1].ref != "v2.0.0-beta.10" || got[1].Label != "2.0 beta" || got[1].Status != "beta" {
		t.Fatalf("beta = %+v", got[1])
	}
	if got[2].ref != "v1.2.10" || !got[2].Latest || got[3].Latest {
		t.Fatalf("latest stable = %+v / %+v", got[2], got[3])
	}

	// A beta older than the newest stable release is not offered.
	if got := selectVersions([]string{"v1.0.0-rc.1", "v1.0.0"}); !reflect.DeepEqual(slugs(got), []string{"next", "1.0"}) {
		t.Fatalf("stale prerelease listed: %v", slugs(got))
	}
	if got := selectVersions(nil); !reflect.DeepEqual(slugs(got), []string{"next"}) || got[0].Latest {
		t.Fatalf("no tags: %v", slugs(got))
	}
	if got := selectVersions([]string{"v1.0.0-alpha"}); !reflect.DeepEqual(slugs(got), []string{"next", "1.0-beta"}) {
		t.Fatalf("prerelease only: %v", slugs(got))
	}
}

func TestCompare(t *testing.T) {
	order := []string{"v1.0.0-alpha", "v1.0.0-alpha.1", "v1.0.0-alpha.beta", "v1.0.0-beta", "v1.0.0-beta.2", "v1.0.0-beta.11", "v1.0.0-rc.1", "v1.0.0", "v1.0.1", "v1.1.0", "v2.0.0"}
	for i := 1; i < len(order); i++ {
		a, _ := parseTag(order[i-1])
		b, _ := parseTag(order[i])
		if a.compare(b) >= 0 || b.compare(a) <= 0 {
			t.Errorf("%s should sort before %s", order[i-1], order[i])
		}
	}
	a, _ := parseTag("v1.2.3-rc.1")
	if a.compare(a) != 0 {
		t.Error("a version differs from itself")
	}
	if _, ok := parseTag("1.2.3"); ok {
		t.Error("tag without the v prefix accepted")
	}
}

func TestRender(t *testing.T) {
	body := "# Commands\n\nSee [multi-profile](multi-profile.md#setup), [architecture](architecture.md) and [the README](../README.md#top).\n"
	out := render("commands", body, Version{Slug: "1.2", Latest: true, ref: "v1.2.9"})
	for _, want := range []string{
		`title: "Commands"`, `docs_version: "1.2"`, `docs_page: "commands"`,
		"](../multi-profile/#setup)",
		"](https://github.com/drumandbytes/eraser/blob/v1.2.9/docs/architecture.md)",
		"](https://github.com/drumandbytes/eraser/blob/v1.2.9/README.md#top)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "# Commands") || strings.Contains(out, "noindex") {
		t.Errorf("heading kept or latest page hidden:\n%s", out)
	}
	out = render("commands", "no heading, see [x](architecture.md)", Version{Slug: "next"})
	if !strings.Contains(out, `title: "commands"`) || !strings.Contains(out, "robots: noindex") || !strings.Contains(out, "/blob/main/") {
		t.Errorf("fallback title, noindex or main ref wrong:\n%s", out)
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@example.com", "-c", "commit.gpgsign=false", "-c", "tag.gpgsign=false"}, args...)...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestRunBuildsSnapshotsFromTags(t *testing.T) {
	repo := t.TempDir()
	runGit(t, repo, "init", "-q")
	write := func(name, body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Join(repo, "docs"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(repo, "docs", name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("commands.md", "# Commands\n\nOld text.\n")
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-q", "-m", "one")
	runGit(t, repo, "tag", "v1.0.0")
	write("commands.md", "# Commands\n\nNew text.\n")
	write("mcp.md", "# MCP\n\nExperimental.\n")
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-q", "-m", "two")
	runGit(t, repo, "tag", "v2.0.0-beta.1")
	write("commands.md", "# Commands\n\nUnreleased text.\n")

	site := filepath.Join(repo, "site")
	// A stale snapshot from an earlier run must disappear; the root index stays.
	if err := os.MkdirAll(filepath.Join(site, "content", "docs", "0.1"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(site, "content", "docs", "_index.md"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := run(repo, site); err != nil {
		t.Fatal(err)
	}

	read := func(rel string) string {
		t.Helper()
		b, err := os.ReadFile(filepath.Join(site, "content", "docs", rel))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	if !strings.Contains(read("1.0/commands.md"), "Old text.") || !strings.Contains(read("2.0-beta/commands.md"), "New text.") || !strings.Contains(read("next/commands.md"), "Unreleased text.") {
		t.Fatal("snapshots do not match their refs")
	}
	if _, err := os.Stat(filepath.Join(site, "content", "docs", "1.0", "mcp.md")); err == nil {
		t.Fatal("page from a later release leaked into 1.0")
	}
	if _, err := os.Stat(filepath.Join(site, "content", "docs", "0.1")); err == nil {
		t.Fatal("stale snapshot kept")
	}
	if read("_index.md") != "keep" || !strings.Contains(read("1.0/_index.md"), `docs_version: "1.0"`) {
		t.Fatal("index pages wrong")
	}

	raw, err := os.ReadFile(filepath.Join(site, "data", "docs_versions.json"))
	if err != nil {
		t.Fatal(err)
	}
	var data struct{ Versions []Version }
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatal(err)
	}
	if got := slugs(data.Versions); !reflect.DeepEqual(got, []string{"next", "2.0-beta", "1.0"}) {
		t.Fatalf("versions = %v", got)
	}
	if !reflect.DeepEqual(data.Versions[1].Pages, []string{"commands", "mcp"}) || !data.Versions[2].Latest {
		t.Fatalf("pages/latest wrong: %+v", data.Versions)
	}
}
