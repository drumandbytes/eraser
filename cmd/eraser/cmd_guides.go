package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"text/template"

	"github.com/drumandbytes/eraser/internal/broker"
	"github.com/drumandbytes/eraser/internal/config"
	emailtmpl "github.com/drumandbytes/eraser/internal/template"
	"github.com/spf13/cobra"
)

// curatedCategories get a full "how to opt out of <broker>" page. Everything
// else (the marketing/adtech long tail) is listed in the directory only -
// hundreds of near-identical thin pages would hurt the site, not help.
var curatedCategories = map[string]bool{
	"people-search":    true,
	"background-check": true,
	"financial-b2b":    true,
	"requires-id":      true,
	"device-id-only":   true,
}

// hasGuide: curated categories always get a page, and a long-tail broker gets one once someone
// has researched it and set guide: true in brokers.yaml.
func hasGuide(b broker.Broker) bool {
	return b.Guide || curatedCategories[strings.ToLower(b.Category)]
}

func guidesCmd() *cobra.Command {
	var (
		output string
		format string
	)

	cmd := &cobra.Command{
		Use: "guides",
		// maintainer tool for the project website; not part of the stable CLI
		Hidden: true,
		Short:  "Generate opt-out guide pages from the broker list",
		Long: `Write a "how to opt out of <broker>" page for each people-search /
background-check / financial / device-ID broker, plus a JSON directory of the
whole list. Feeds the documentation site.

--format md  (default) writes Hugo content (content/brokers/*.md + data/brokers.json)
--format html writes self-contained pages that open offline`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGuides(output, format)
		},
	}

	cmd.Flags().StringVarP(&output, "output", "o", "site/content", "output directory")
	cmd.Flags().StringVar(&format, "format", "md", "md (Hugo content) or html (standalone pages)")

	return cmd
}

// placeholderProfile renders the request templates with fill-in-the-blank
// values instead of a real person's details.
func placeholderProfile() config.Profile {
	return config.Profile{
		FirstName: "[your first name]",
		LastName:  "[your last name]",
		Email:     "[your email address]",
		Address:   "[your street address]",
		City:      "[your city]",
		State:     "[your state/province]",
		ZipCode:   "[your postal code]",
		Country:   "[your country]",
	}
}

var dateInText = regexp.MustCompile(`\b(\d{4}-\d{2}-\d{2})\b`)

type guidePage struct {
	Broker      broker.Broker
	LastChecked string // extracted from Notes, if any
	Description string
	Timeline    string
	Noindex     bool
	GDPRBody    string
	CCPABody    string

	CategoryLabel string
	CategoryIntro string
	Law           string
	Route         string
	NeedsID       bool
	Related       []relatedGuide
}

type relatedGuide struct{ ID, Name string }

// noindexedBrokers: reachable but kept out of search and the sitemap, as they
// offer nothing actionable. Curated by hand; a dead end is an editorial call.
var noindexedBrokers = map[string]bool{
	"publicrecordsnow": true, // no email or opt-out URL on file; the one we had bounced
	"regis24":          true, // DE/AT-only credit bureau; explicit no-further-action reply
}

// timelineText names the deadline a broker's reply is legally bound by.
// Region-gated: previously this sentence cited both GDPR's one month and the
// CCPA's 45 days on every page, including e.g. a German-only credit bureau
// with no CCPA exposure at all.
func timelineText(region string) string {
	switch region {
	case "eu":
		return "Under GDPR they must respond within one month."
	case "us":
		return "Under the CCPA, they must respond within 45 days."
	default: // "global" - could be either, depending on the requester
		return "Under GDPR they must respond within one month if you're in the EU/EEA/UK, or within 45 days under the CCPA if you're in the US."
	}
}

// categoryInfo explains, per curated category, what these brokers do and what removal looks
// like. Shared by every page in the category, so keep it general and true of all of them.
var categoryInfo = map[string]struct{ Label, Intro string }{
	"marketing":        {"Marketing-data broker", "Marketing-data brokers compile consumer or business contact data and audience segments for advertisers, usually without a public search site. Removal means an opt-out or deletion request to the company itself, and you may need to repeat it for each brand or affiliate it operates."},
	"people-search":    {"People-search site", "People-search sites assemble public records (addresses, phone numbers, ages, relatives) into searchable profiles. Most have a web form, and many ask you to confirm by email. Profiles often reappear after a few months as the underlying records are re-ingested, so check again every 60-90 days."},
	"background-check": {"Background-check company", "Background-check and screening companies sell reports built from court, address and employment records to employers, landlords and the public. Some are consumer reporting agencies, which gives you access and dispute rights as well as deletion, and the law can require them to keep some records. Ask for access first if you want to see what they hold."},
	"financial-b2b":    {"Credit bureau or business-data provider", "Credit bureaus and business-data providers hold financial and company-contact data. Credit reporting is regulated, so erasure is often limited to data held beyond what the law requires; access, correction and objection rights usually still apply. Expect a longer, more formal reply than from a people-search site."},
	"requires-id":      {"Broker that verifies identity first", "These brokers ask you to prove who you are before they act, usually with a copy of an ID or a verification form, so that nobody else can delete your record. Send only what their own page asks for, and cover or redact anything they do not need."},
	"device-id-only":   {"Mobile and device-data broker", "These companies hold mobile advertising IDs, location and app data rather than names. Their forms usually need your device's advertising ID, so reset or delete it in your phone's privacy settings as well, otherwise a new ID starts collecting again."},
}

// lawText names the law that gives the requester a deletion right, by broker region.
func lawText(region string) string {
	switch region {
	case "eu":
		return "EU / EEA (GDPR Article 17)"
	case "us":
		return "United States (CCPA / CPRA and other state laws)"
	default:
		return "Global (GDPR Article 17 for EU/EEA/UK residents, CCPA / CPRA for California)"
	}
}

func routeText(b broker.Broker) string {
	switch {
	case b.OptOutURL != "" && b.Email != "":
		return "Web form, with an email fallback"
	case b.OptOutURL != "":
		return "Web form"
	case b.Email != "":
		return "Email"
	default:
		return "No public channel on file"
	}
}

// relatedGuides returns up to n other guides from the same category: the ones that follow b in
// name order, wrapping around, so every guide in a category gets about the same number of
// incoming links. Noindexed brokers are not linked to.
func relatedGuides(b broker.Broker, curated []broker.Broker, n int) []relatedGuide {
	var same []broker.Broker
	idx := -1
	for _, o := range curated {
		if !strings.EqualFold(o.Category, b.Category) {
			continue
		}
		if o.ID == b.ID {
			idx = len(same)
		}
		same = append(same, o)
	}
	var out []relatedGuide
	for i := 1; i < len(same) && len(out) < n; i++ {
		o := same[(idx+i)%len(same)]
		if noindexedBrokers[o.ID] {
			continue
		}
		out = append(out, relatedGuide{o.ID, o.Name})
	}
	return out
}

// guideDescription is the meta description / JSON-LD description for a
// broker page - previously the site-wide default, verbatim, on every page.
func guideDescription(b broker.Broker, lastChecked string, hasLetter bool) string {
	var method string
	switch {
	case b.OptOutURL != "":
		method = "submit their opt-out form"
	case b.Email != "":
		method = "email a data-removal request"
	default:
		method = "contact them directly"
	}

	var window string
	switch b.Region {
	case "eu":
		window = "GDPR gives them one month to respond"
	case "us":
		window = "CCPA gives them 45 days to respond"
	default:
		window = "GDPR or CCPA applies depending on your location"
	}

	desc := fmt.Sprintf("How to opt out of %s: %s. %s.", b.Name, method, window)
	if hasLetter {
		desc += " Includes a ready-to-send request letter."
	}
	if lastChecked != "" {
		desc = fmt.Sprintf("%s Checked %s.", desc, lastChecked)
	}
	return truncateRunes(desc, 160)
}

// truncateRunes cuts by rune, not byte, count -- broker names carry accented
// characters (Buró, Crédito, Investigación), and byte-slicing a UTF-8 string
// can split one in half.
func truncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return strings.TrimSpace(string(r[:max-1])) + "…"
}

func runGuides(outDir, format string) error {
	switch format {
	case "md", "html":
	default:
		return fmt.Errorf("unknown --format %q (want md or html)", format)
	}

	db, err := broker.Load(brokerFile)
	if err != nil {
		return fmt.Errorf("failed to load brokers: %w", err)
	}
	engine, err := emailtmpl.NewEngine()
	if err != nil {
		return fmt.Errorf("failed to init template engine: %w", err)
	}
	pp := placeholderProfile()

	brokersDir := filepath.Join(outDir, "brokers")
	if err := os.MkdirAll(brokersDir, 0o755); err != nil {
		return err
	}

	var curated []broker.Broker
	for _, b := range db.Brokers {
		if hasGuide(b) {
			curated = append(curated, b)
		}
	}
	sort.Slice(curated, func(i, j int) bool { return curated[i].Name < curated[j].Name })

	written := 0
	for _, b := range curated {
		page := guidePage{Broker: b}
		if m := dateInText.FindStringSubmatch(b.Notes); m != nil {
			page.LastChecked = m[1]
		}
		page.Timeline = timelineText(b.Region)
		cat := strings.ToLower(b.Category)
		page.CategoryLabel, page.CategoryIntro = categoryInfo[cat].Label, categoryInfo[cat].Intro
		page.Law, page.Route, page.NeedsID = lawText(b.Region), routeText(b), cat == "requires-id"
		page.Related = relatedGuides(b, curated, 5)
		page.Noindex = noindexedBrokers[b.ID]
		if gdpr, err := engine.Render("gdpr", pp, b); err == nil {
			page.GDPRBody = gdpr.Body
		}
		if ccpa, err := engine.Render("ccpa", pp, b); err == nil {
			page.CCPABody = ccpa.Body
		}
		page.Description = guideDescription(b, page.LastChecked, page.GDPRBody != "" || page.CCPABody != "")

		var (
			content []byte
			ext     string
		)
		if format == "md" {
			content, err = renderGuideMarkdown(page)
			ext = ".md"
		} else {
			content, err = renderGuideHTML(page)
			ext = ".html"
		}
		if err != nil {
			return fmt.Errorf("rendering %s: %w", b.ID, err)
		}
		if err := os.WriteFile(filepath.Join(brokersDir, b.ID+ext), content, 0o644); err != nil {
			return err
		}
		written++
	}

	// JSON directory of the entire list (curated + long tail) for the site's
	// filterable directory page - written into the site's static/ dir so the
	// browser can fetch it directly (a top-level JSON array isn't a valid Hugo
	// data file).
	dataDir := filepath.Join(filepath.Dir(strings.TrimRight(outDir, "/")), "static")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return err
	}
	type dirEntry struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Region   string `json:"region"`
		Category string `json:"category"`
		Email    string `json:"email,omitempty"`
		OptOut   string `json:"opt_out_url,omitempty"`
		Curated  bool   `json:"curated"`
		Notes    string `json:"notes,omitempty"`
	}
	dir := make([]dirEntry, 0, len(db.Brokers))
	for _, b := range db.Brokers {
		dir = append(dir, dirEntry{
			ID: b.ID, Name: b.Name, Region: b.Region, Category: b.Category,
			Email: b.Email, OptOut: b.OptOutURL,
			Curated: hasGuide(b), Notes: b.Notes,
		})
	}
	sort.Slice(dir, func(i, j int) bool { return strings.ToLower(dir[i].Name) < strings.ToLower(dir[j].Name) })
	jsonBytes, _ := json.MarshalIndent(dir, "", "  ")
	if err := os.WriteFile(filepath.Join(dataDir, "brokers.json"), jsonBytes, 0o644); err != nil {
		return err
	}

	// Hugo reads the same list from site/data to server-render the full directory, so the
	// long tail is in the HTML and not only in what directory.js fetches.
	hugoData := filepath.Join(filepath.Dir(dataDir), "data")
	if err := os.MkdirAll(hugoData, 0o755); err != nil {
		return err
	}
	wrapped, _ := json.MarshalIndent(map[string]any{"brokers": dir}, "", "  ")
	if err := os.WriteFile(filepath.Join(hugoData, "brokers.json"), wrapped, 0o644); err != nil {
		return err
	}

	fmt.Println("📚 Guides generated")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Printf("   %d guide page(s) in %s\n", written, brokersDir)
	fmt.Printf("   directory of %d brokers in %s\n", len(dir), filepath.Join(dataDir, "brokers.json"))
	return nil
}

func renderGuideMarkdown(p guidePage) ([]byte, error) {
	var buf strings.Builder
	if err := guideMD.Execute(&buf, p); err != nil {
		return nil, err
	}
	return []byte(buf.String()), nil
}

func renderGuideHTML(p guidePage) ([]byte, error) {
	var buf strings.Builder
	if err := guideHTML.Execute(&buf, p); err != nil {
		return nil, err
	}
	return []byte(buf.String()), nil
}

var guideFuncs = template.FuncMap{
	"lower": strings.ToLower,
	"title": func(s string) string {
		words := strings.Fields(strings.ReplaceAll(s, "-", " "))
		for i, w := range words {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
		return strings.Join(words, " ")
	},
}

var guideMD = template.Must(template.New("md").Funcs(guideFuncs).Parse(`---
title: "How to opt out of {{.Broker.Name}}"
description: "{{.Description}}"
broker_id: "{{.Broker.ID}}"
region: "{{.Broker.Region}}"
category: "{{.Broker.Category}}"
{{if .Broker.Email}}email: "{{.Broker.Email}}"{{end}}
{{if .Broker.OptOutURL}}opt_out_url: "{{.Broker.OptOutURL}}"{{end}}
{{if .LastChecked}}last_checked: "{{.LastChecked}}"{{end}}
{{if .Noindex}}robots: "noindex,follow"
sitemap:
  disable: true
{{end}}
---

**{{.Broker.Name}}** is a {{title .Broker.Category}} data broker{{if eq .Broker.Region "eu"}} (EU){{else if eq .Broker.Region "us"}} (US){{end}}.
{{if .Broker.Notes}}

> {{.Broker.Notes}}
{{end}}

## At a glance

- **Type:** {{.CategoryLabel}}
- **Law that applies:** {{.Law}}
- **How to opt out:** {{.Route}}
- **Response deadline:** {{.Timeline}}{{if .Broker.Parent}}
- **Operated by:** {{.Broker.Parent}}{{if .Broker.HQ}}, {{.Broker.HQ}}{{end}}{{else if .Broker.HQ}}
- **Based in:** {{.Broker.HQ}}{{end}}{{if .Broker.Holds}}
- **Data they say they hold:** {{.Broker.Holds}}{{end}}{{if .Broker.Verification}}
- **Identity check:** {{.Broker.Verification}}{{else if .NeedsID}}
- **Identity check:** expect to be asked for proof of identity{{end}}{{if .Broker.ResponseTime}}
- **Response time in their policy:** {{.Broker.ResponseTime}}{{end}}

## Opt out

{{if .Broker.OptOutURL}}
1. Go to **[{{.Broker.OptOutURL}}]({{.Broker.OptOutURL}})**.
2. Follow their removal / suppression process. You may need to confirm by email or verify your identity.
{{if .Broker.Email}}3. If the form fails, email **{{.Broker.Email}}** with the request below.{{end}}
{{else if .Broker.Email}}
Email **{{.Broker.Email}}** with the request below.
{{else}}
No public opt-out URL or email is on file for this broker yet. Try their website's privacy page, or contact them and cite the laws below.
{{end}}

{{if or .GDPRBody .CCPABody}}
<details>
<summary>Request letter templates</summary>

{{if .GDPRBody}}
### GDPR request (EU/EEA residents)

` + "```" + `
{{.GDPRBody}}
` + "```" + `
{{end}}
{{if .CCPABody}}
### CCPA request (California residents)

` + "```" + `
{{.CCPABody}}
` + "```" + `
{{end}}

</details>
{{end}}
{{if .CategoryIntro}}
## About {{.CategoryLabel | lower}}s

{{.CategoryIntro}}
{{end}}
{{if .Broker.Sources}}
## Sources

Facts above come from the broker's own pages{{if .Broker.Researched}}, read on {{.Broker.Researched}}{{end}}:

{{range .Broker.Sources}}- <{{.}}>
{{end}}{{end}}
{{if .Related}}
## Related guides

{{range .Related}}- [How to opt out of {{.Name}}](/brokers/{{.ID}}/)
{{end}}{{end}}
## Related reading

- [Sending a GDPR Article 17 request](/guides/sending-a-gdpr-article-17-request/)
- [When a broker ignores you](/guides/when-a-broker-ignores-you/)
- [Opting out of advertising data brokers](/guides/opting-out-of-advertising-data-brokers/)
- [All brokers Eraser covers](/brokers/)

---

*Fill in the bracketed placeholders before sending. [Eraser](https://github.com/drumandbytes/eraser) can send this and track the reply for you, or you can send it yourself.*
`))

var guideHTML = template.Must(template.New("html").Funcs(guideFuncs).Parse(`<!DOCTYPE html>
<html lang="en"><head><meta charset="utf-8">
<title>How to opt out of {{.Broker.Name}}</title>
<style>body{font:15px/1.6 -apple-system,"Segoe UI",sans-serif;max-width:780px;margin:2rem auto;padding:0 1rem;color:#1a1a1a}
h1{font-size:1.5rem}pre{white-space:pre-wrap;background:#f6f6f6;border:1px solid #e2e2e2;padding:1rem;border-radius:6px;font-size:.85rem;overflow-x:auto}
blockquote{border-left:3px solid #ccc;margin:1rem 0;padding-left:1rem;color:#555}a{color:#2657c8}</style>
</head><body>
<h1>How to opt out of {{.Broker.Name}}</h1>
<p><strong>{{.Broker.Name}}</strong> is a {{title .Broker.Category}} data broker{{if .Broker.Region}} ({{.Broker.Region}}){{end}}.</p>
{{if .Broker.Notes}}<blockquote>{{.Broker.Notes}}</blockquote>{{end}}
<h2>Opt out</h2>
{{if .Broker.OptOutURL}}<ol><li>Go to <a href="{{.Broker.OptOutURL}}">{{.Broker.OptOutURL}}</a>.</li>
<li>Follow their removal process (you may need to confirm by email).</li>
{{if .Broker.Email}}<li>If the form fails, email <strong>{{.Broker.Email}}</strong> with the request below.</li>{{end}}</ol>
{{else if .Broker.Email}}<p>Email <strong>{{.Broker.Email}}</strong> with the request below.</p>
{{else}}<p>No public opt-out URL or email on file yet - try their website's privacy page.</p>{{end}}
{{if .GDPRBody}}<h3>GDPR request (EU/EEA)</h3><pre>{{.GDPRBody}}</pre>{{end}}
{{if .CCPABody}}<h3>CCPA request (California)</h3><pre>{{.CCPABody}}</pre>{{end}}
<hr><p><em>Fill in the bracketed placeholders before sending.</em></p>
</body></html>
`))
