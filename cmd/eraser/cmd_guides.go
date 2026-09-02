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

	"github.com/eraser-privacy/eraser/internal/broker"
	"github.com/eraser-privacy/eraser/internal/config"
	emailtmpl "github.com/eraser-privacy/eraser/internal/template"
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

func guidesCmd() *cobra.Command {
	var (
		output string
		format string
	)

	cmd := &cobra.Command{
		Use:   "guides",
		Short: "Generate opt-out guide pages from the broker list",
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
	GDPRBody    string
	CCPABody    string
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
		if curatedCategories[strings.ToLower(b.Category)] {
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
		if gdpr, err := engine.Render("gdpr", pp, b); err == nil {
			page.GDPRBody = gdpr.Body
		}
		if ccpa, err := engine.Render("ccpa", pp, b); err == nil {
			page.CCPABody = ccpa.Body
		}

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
	// filterable directory page.
	dataDir := filepath.Join(filepath.Dir(strings.TrimRight(outDir, "/")), "data")
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
			Curated: curatedCategories[strings.ToLower(b.Category)], Notes: b.Notes,
		})
	}
	sort.Slice(dir, func(i, j int) bool { return strings.ToLower(dir[i].Name) < strings.ToLower(dir[j].Name) })
	jsonBytes, _ := json.MarshalIndent(dir, "", "  ")
	if err := os.WriteFile(filepath.Join(dataDir, "brokers.json"), jsonBytes, 0o644); err != nil {
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
broker_id: "{{.Broker.ID}}"
region: "{{.Broker.Region}}"
category: "{{.Broker.Category}}"
{{if .Broker.Email}}email: "{{.Broker.Email}}"{{end}}
{{if .Broker.OptOutURL}}opt_out_url: "{{.Broker.OptOutURL}}"{{end}}
{{if .LastChecked}}last_checked: "{{.LastChecked}}"{{end}}
---

**{{.Broker.Name}}** is a {{title .Broker.Category}} data broker{{if eq .Broker.Region "eu"}} (EU){{else if eq .Broker.Region "us"}} (US){{end}}.
{{if .Broker.Notes}}

> {{.Broker.Notes}}
{{end}}

## Opt out

{{if .Broker.OptOutURL}}
1. Go to **[{{.Broker.OptOutURL}}]({{.Broker.OptOutURL}})**.
2. Follow their removal / suppression process. You may need to confirm by email or verify your identity.
{{if .Broker.Email}}3. If the form fails, email **{{.Broker.Email}}** with the request below.{{end}}
{{else if .Broker.Email}}
Email **{{.Broker.Email}}** with the request below. Under GDPR they must respond within one month; under the CCPA, within 45 days.
{{else}}
No public opt-out URL or email is on file for this broker yet. Try their website's privacy page, or contact them and cite the laws below.
{{end}}

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
