package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/drumandbytes/eraser/internal/broker"
	"github.com/drumandbytes/eraser/internal/config"
	"github.com/drumandbytes/eraser/internal/dpa"
	"github.com/drumandbytes/eraser/internal/history"
	emailtmpl "github.com/drumandbytes/eraser/internal/template"
	"github.com/spf13/cobra"
)

func exportCmd() *cobra.Command {
	var (
		output string
		format string
		since  string
	)

	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export an evidence report of every removal request and response",
		Long: `Build a single document recording, per broker: what removal request was
sent (date, recipient, legal basis, a reconstruction of the email), what the
broker replied and when, the current pipeline stage, and whether the statutory
response deadline has passed with no substantive reply.

Intended as the attachment for a complaint to a data protection authority or to
noyb.eu when a controller ignores a GDPR Article 17 request. HTML opens in a
browser and prints to PDF; JSON is the same data unformatted.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runExport(exportOptions{output: output, format: format, since: since})
		},
	}

	cmd.Flags().StringVarP(&output, "output", "o", "", "output file (default: eraser-evidence-<profile>-<date>.<ext>)")
	cmd.Flags().StringVar(&format, "format", "html", "output format: html or json")
	cmd.Flags().StringVar(&since, "since", "", "only include requests sent on or after this date (YYYY-MM-DD)")

	return cmd
}

type exportOptions struct {
	output string
	format string
	since  string
}

func runExport(opts exportOptions) error {
	switch opts.format {
	case "html", "json":
	default:
		return fmt.Errorf("unknown --format %q (want html or json)", opts.format)
	}

	var sinceTime time.Time
	if opts.since != "" {
		t, err := time.Parse("2006-01-02", opts.since)
		if err != nil {
			return fmt.Errorf("invalid --since %q (want YYYY-MM-DD): %w", opts.since, err)
		}
		sinceTime = t
	}

	cfg, err := config.Load(resolveConfigPath())
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	activeProfile, err := resolveProfile(cfg)
	if err != nil {
		return err
	}

	store, err := history.NewStore(history.DBPathFor(resolveConfigPath()))
	if err != nil {
		return fmt.Errorf("failed to open history: %w", err)
	}
	defer func() { _ = store.Close() }()

	requests, err := store.GetAllRequests(activeProfile.ID)
	if err != nil {
		return fmt.Errorf("failed to read requests: %w", err)
	}
	responses, err := store.GetBrokerResponsesForExport(activeProfile.ID)
	if err != nil {
		return fmt.Errorf("failed to read responses: %w", err)
	}

	brokerDB, err := broker.Load(brokerFile)
	if err != nil {
		return fmt.Errorf("failed to load brokers: %w", err)
	}
	engine, err := emailtmpl.NewEngine()
	if err != nil {
		return fmt.Errorf("failed to init template engine: %w", err)
	}

	report := buildEvidenceReport(activeProfile.Profile, requests, responses, brokerDB, engine, sinceTime, time.Now())

	path := opts.output
	if path == "" {
		ext := opts.format
		path = fmt.Sprintf("eraser-evidence-%s-%s.%s", activeProfile.ID, time.Now().Format("2006-01-02"), ext)
	}

	var data []byte
	if opts.format == "json" {
		data, err = json.MarshalIndent(report, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to encode JSON: %w", err)
		}
	} else {
		data, err = renderEvidenceHTML(report)
		if err != nil {
			return fmt.Errorf("failed to render HTML: %w", err)
		}
	}

	// The report contains the data subject's identity details - same 0600 as
	// every other personal-data file this tool writes.
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("failed to write %s: %w", path, err)
	}

	fmt.Println("📄 Evidence report written")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	if len(cfg.GetProfiles()) > 1 {
		fmt.Printf("👤 Profile: %s\n", activeProfile.ID)
	}
	fmt.Printf("   File:     %s\n", path)
	fmt.Printf("   Brokers:  %d contacted, %d replied\n", report.Summary.BrokersContacted, report.Summary.BrokersResponded)
	fmt.Printf("   Requests: %d sent, %d failed\n", report.Summary.Sent, report.Summary.Failed)
	if n := len(report.Summary.PastDeadline); n > 0 {
		fmt.Printf("   ⚠️  %d broker(s) past the response deadline with no substantive reply:\n", n)
		for _, name := range report.Summary.PastDeadline {
			fmt.Printf("        - %s\n", name)
		}
		if report.Authority != nil {
			fmt.Printf("   Complain to: %s\n", report.Authority.Describe())
		}
	}
	return nil
}

// ---- report model ----

type EvidenceReport struct {
	GeneratedAt time.Time        `json:"generated_at"`
	ProfileID   string           `json:"profile_id"`
	Subject     DataSubject      `json:"data_subject"`
	Brokers     []BrokerEvidence `json:"brokers"`
	Summary     EvidenceSummary  `json:"summary"`
	// Authority is the supervisory authority for the data subject's country,
	// when it could be resolved - the place to lodge an Art. 77 complaint.
	Authority *dpa.Authority `json:"supervisory_authority,omitempty"`
}

type DataSubject struct {
	FullName          string   `json:"full_name"`
	Email             string   `json:"email"`
	DateOfBirth       string   `json:"date_of_birth,omitempty"`
	Phone             string   `json:"phone,omitempty"`
	Address           string   `json:"address,omitempty"`
	NameVariants      []string `json:"name_variants,omitempty"`
	AdditionalEmails  []string `json:"additional_emails,omitempty"`
	AdditionalPhones  []string `json:"additional_phones,omitempty"`
	PreviousAddresses []string `json:"previous_addresses,omitempty"`
}

type BrokerEvidence struct {
	BrokerID       string             `json:"broker_id"`
	BrokerName     string             `json:"broker_name"`
	BrokerEmail    string             `json:"broker_email,omitempty"`
	BrokerWebsite  string             `json:"broker_website,omitempty"`
	PipelineStatus string             `json:"pipeline_status,omitempty"`
	PastDeadline   bool               `json:"past_deadline"`
	DeadlineDate   *time.Time         `json:"deadline_date,omitempty"`
	Requests       []RequestEvidence  `json:"requests"`
	Responses      []ResponseEvidence `json:"responses"`
}

type RequestEvidence struct {
	SentAt          time.Time `json:"sent_at"`
	Recipient       string    `json:"recipient"`
	Template        string    `json:"template"`
	LegalBasis      string    `json:"legal_basis"`
	MessageID       string    `json:"message_id,omitempty"`
	Status          string    `json:"status"`
	Manual          bool      `json:"sent_manually"`
	Error           string    `json:"error,omitempty"`
	RenderedSubject string    `json:"reconstructed_subject,omitempty"`
	RenderedBody    string    `json:"reconstructed_body,omitempty"`
}

type ResponseEvidence struct {
	ReceivedAt  time.Time `json:"received_at"`
	Type        string    `json:"type"`
	From        string    `json:"from,omitempty"`
	Subject     string    `json:"subject,omitempty"`
	Confidence  float64   `json:"confidence"`
	Body        string    `json:"body,omitempty"`
	FormURL     string    `json:"form_url,omitempty"`
	ConfirmURL  string    `json:"confirm_url,omitempty"`
	NeedsReview bool      `json:"needs_review"`
}

type EvidenceSummary struct {
	TotalRequests    int      `json:"total_requests"`
	Sent             int      `json:"sent"`
	Failed           int      `json:"failed"`
	BrokersContacted int      `json:"brokers_contacted"`
	BrokersResponded int      `json:"brokers_responded"`
	PastDeadline     []string `json:"past_deadline_no_reply"`
}

// legalBasisFor returns the citation and statutory deadline for a template name.
func legalBasisFor(templateName string) string {
	switch templateName {
	case "gdpr":
		return "GDPR Article 17 (Right to Erasure). The controller must respond within one month (Article 12(3))."
	case "ccpa":
		return "CCPA / California Civil Code § 1798.105 (Right to Delete). The business must respond within 45 days (§ 1798.130)."
	default:
		return "Multiple privacy laws (GDPR, CCPA/CPRA, VCDPA, CPA). A response within 30 days was requested."
	}
}

// substantiveResponse reports whether a response type counts as the controller
// actually engaging with the request (vs. silence, a bounce, or noise).
func substantiveResponse(responseType string) bool {
	switch responseType {
	case "success", "rejected", "form_required", "confirmation_required":
		return true
	default:
		return false
	}
}

// deadlineFor returns the statutory response deadline for a request.
func deadlineFor(templateName string, sentAt time.Time) time.Time {
	switch templateName {
	case "ccpa":
		return sentAt.AddDate(0, 0, 45)
	case "gdpr":
		return sentAt.AddDate(0, 1, 0)
	default:
		return sentAt.AddDate(0, 0, 30)
	}
}

func buildEvidenceReport(
	profile config.Profile,
	requests []history.Record,
	responses []history.BrokerResponse,
	brokerDB *broker.BrokerDatabase,
	engine *emailtmpl.Engine,
	since time.Time,
	now time.Time,
) EvidenceReport {
	report := EvidenceReport{
		GeneratedAt: now,
		Authority:   dpa.ForCountry(profile.Country),
		Subject: DataSubject{
			FullName:          profile.FullName(),
			Email:             profile.Email,
			DateOfBirth:       profile.DateOfBirth,
			Phone:             profile.Phone,
			Address:           joinAddress(profile),
			NameVariants:      profile.NameVariants,
			AdditionalEmails:  profile.AdditionalEmails,
			AdditionalPhones:  profile.AdditionalPhones,
			PreviousAddresses: profile.PreviousAddresses,
		},
	}

	responsesByBroker := map[string][]history.BrokerResponse{}
	for _, r := range responses {
		responsesByBroker[r.BrokerID] = append(responsesByBroker[r.BrokerID], r)
	}

	// Group requests by broker, preserving first-seen order.
	var order []string
	byBroker := map[string][]history.Record{}
	for _, req := range requests {
		if !since.IsZero() && req.SentAt.Before(since) {
			continue
		}
		if _, seen := byBroker[req.BrokerID]; !seen {
			order = append(order, req.BrokerID)
		}
		byBroker[req.BrokerID] = append(byBroker[req.BrokerID], req)
	}

	pastDeadlineSet := map[string]bool{}
	for _, brokerID := range order {
		reqs := byBroker[brokerID]
		be := BrokerEvidence{BrokerID: brokerID}
		if b := brokerDB.FindByID(brokerID); b != nil {
			be.BrokerName = b.Name
			be.BrokerEmail = b.Email
			be.BrokerWebsite = b.Website
		}
		if be.BrokerName == "" && len(reqs) > 0 {
			be.BrokerName = reqs[0].BrokerName
		}

		var latestPipeline history.PipelineStatus
		var earliestSent time.Time
		var anySent bool
		for _, req := range reqs {
			re := RequestEvidence{
				SentAt:     req.SentAt,
				Recipient:  req.Email,
				Template:   req.Template,
				LegalBasis: legalBasisFor(req.Template),
				MessageID:  req.MessageID,
				Status:     string(req.Status),
				Manual:     req.SentMethod == "manual",
				Error:      req.Error,
			}
			if b := brokerDB.FindByID(brokerID); b != nil {
				if email, err := engine.Render(req.Template, profile, *b); err == nil {
					re.RenderedSubject = email.Subject
					re.RenderedBody = email.Body
				}
			}
			be.Requests = append(be.Requests, re)
			if req.PipelineStatus != "" {
				latestPipeline = req.PipelineStatus
			}
			if req.Status == history.StatusSent && !req.SentAt.IsZero() {
				if !anySent || req.SentAt.Before(earliestSent) {
					earliestSent = req.SentAt
					anySent = true
				}
			}
		}
		be.PipelineStatus = string(latestPipeline)

		resp := responsesByBroker[brokerID]
		sort.SliceStable(resp, func(i, j int) bool { return resp[i].ReceivedAt.Before(resp[j].ReceivedAt) })
		gotSubstantive := false
		for _, r := range resp {
			be.Responses = append(be.Responses, ResponseEvidence{
				ReceivedAt:  r.ReceivedAt,
				Type:        r.ResponseType,
				From:        r.EmailFrom,
				Subject:     r.EmailSubject,
				Confidence:  r.Confidence,
				Body:        r.EmailBody,
				FormURL:     r.FormURL,
				ConfirmURL:  r.ConfirmURL,
				NeedsReview: r.NeedsReview,
			})
			if substantiveResponse(r.ResponseType) {
				gotSubstantive = true
			}
		}

		if anySent {
			deadline := deadlineFor(reqs[0].Template, earliestSent)
			be.DeadlineDate = &deadline
			if !gotSubstantive && now.After(deadline) {
				be.PastDeadline = true
				if !pastDeadlineSet[be.BrokerName] {
					pastDeadlineSet[be.BrokerName] = true
					report.Summary.PastDeadline = append(report.Summary.PastDeadline, be.BrokerName)
				}
			}
		}

		report.Brokers = append(report.Brokers, be)
	}

	for _, req := range requests {
		if !since.IsZero() && req.SentAt.Before(since) {
			continue
		}
		report.Summary.TotalRequests++
		switch req.Status {
		case history.StatusSent:
			report.Summary.Sent++
		case history.StatusFailed:
			report.Summary.Failed++
		}
	}
	report.Summary.BrokersContacted = len(report.Brokers)
	for _, be := range report.Brokers {
		if len(be.Responses) > 0 {
			report.Summary.BrokersResponded++
		}
	}
	return report
}

func joinAddress(p config.Profile) string {
	parts := []string{}
	for _, s := range []string{p.Address, p.City, p.State, p.ZipCode, p.Country} {
		if strings.TrimSpace(s) != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, ", ")
}

func renderEvidenceHTML(report EvidenceReport) ([]byte, error) {
	t, err := template.New("evidence").Funcs(template.FuncMap{
		"date":     func(t time.Time) string { return t.Format("2 January 2006") },
		"datetime": func(t time.Time) string { return t.Format("2 January 2006, 15:04 MST") },
		"pct":      func(f float64) string { return fmt.Sprintf("%.0f%%", f*100) },
		"nonzero":  func(t time.Time) bool { return !t.IsZero() },
	}).Parse(evidenceHTMLTemplate)
	if err != nil {
		return nil, err
	}
	var buf strings.Builder
	if err := t.Execute(&buf, report); err != nil {
		return nil, err
	}
	return []byte(buf.String()), nil
}

const evidenceHTMLTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>Data removal evidence report - {{.Subject.FullName}}</title>
<style>
  body { font: 15px/1.55 -apple-system, "Segoe UI", Roboto, sans-serif; color: #1a1a1a; max-width: 820px; margin: 2rem auto; padding: 0 1rem; }
  h1 { font-size: 1.5rem; margin-bottom: .25rem; }
  h2 { font-size: 1.15rem; margin-top: 2.5rem; border-bottom: 2px solid #ddd; padding-bottom: .3rem; }
  h3 { font-size: 1rem; margin: 1.5rem 0 .4rem; }
  .muted { color: #666; }
  .meta { color: #666; font-size: .9rem; margin-bottom: 2rem; }
  dl { display: grid; grid-template-columns: max-content 1fr; gap: .2rem 1rem; margin: .5rem 0; }
  dt { font-weight: 600; }
  .broker { border: 1px solid #e0e0e0; border-radius: 6px; padding: 1rem 1.25rem; margin: 1rem 0; }
  .broker.past-deadline { border-color: #c0392b; background: #fdf3f2; }
  .flag { display: inline-block; background: #c0392b; color: #fff; font-size: .75rem; padding: .1rem .5rem; border-radius: 3px; vertical-align: middle; }
  .event { border-left: 3px solid #ccc; padding: .3rem 0 .3rem .8rem; margin: .5rem 0; }
  .event.sent { border-color: #2980b9; }
  .event.failed { border-color: #c0392b; }
  .event.response { border-color: #27ae60; }
  pre { white-space: pre-wrap; background: #f7f7f7; border: 1px solid #eee; padding: .75rem; border-radius: 4px; font-size: .85rem; overflow-x: auto; }
  details { margin: .4rem 0; }
  summary { cursor: pointer; color: #2980b9; }
  table.summary { border-collapse: collapse; margin: 1rem 0; }
  table.summary td { padding: .2rem 1rem .2rem 0; }
  .disclaimer { font-size: .85rem; color: #666; font-style: italic; }
  @media print { .broker { break-inside: avoid; } }
</style>
</head>
<body>
<h1>Data removal request - evidence report</h1>
<p class="meta">Generated {{datetime .GeneratedAt}}{{if .ProfileID}} &middot; profile <code>{{.ProfileID}}</code>{{end}}</p>

<h2>Data subject</h2>
<dl>
  <dt>Name</dt><dd>{{.Subject.FullName}}{{range .Subject.NameVariants}} &middot; also {{.}}{{end}}</dd>
  <dt>Email</dt><dd>{{.Subject.Email}}{{range .Subject.AdditionalEmails}} &middot; {{.}}{{end}}</dd>
  {{if .Subject.Address}}<dt>Address</dt><dd>{{.Subject.Address}}</dd>{{end}}
  {{range .Subject.PreviousAddresses}}<dt>Former address</dt><dd>{{.}}</dd>{{end}}
  {{if .Subject.DateOfBirth}}<dt>Date of birth</dt><dd>{{.Subject.DateOfBirth}}</dd>{{end}}
  {{if .Subject.Phone}}<dt>Phone</dt><dd>{{.Subject.Phone}}{{range .Subject.AdditionalPhones}} &middot; {{.}}{{end}}</dd>{{end}}
</dl>

<h2>Summary</h2>
<table class="summary">
  <tr><td>Requests sent</td><td><strong>{{.Summary.Sent}}</strong> (of {{.Summary.TotalRequests}}; {{.Summary.Failed}} failed to send)</td></tr>
  <tr><td>Brokers contacted</td><td><strong>{{.Summary.BrokersContacted}}</strong></td></tr>
  <tr><td>Brokers that replied</td><td><strong>{{.Summary.BrokersResponded}}</strong></td></tr>
  <tr><td>Past deadline, no substantive reply</td><td><strong>{{len .Summary.PastDeadline}}</strong></td></tr>
</table>
{{if .Summary.PastDeadline}}
<p>The following controllers have not substantively responded within the statutory
period and are candidates for a complaint to a supervisory authority
{{if .Authority}}(for {{.Authority.Country}}: <strong>{{.Authority.Authority}}</strong>{{if .Authority.Acronym}} ({{.Authority.Acronym}}){{end}}{{with .Authority.Law}}, under {{.}}{{end}} &ndash; <a href="{{.Authority.Website}}">{{.Authority.Website}}</a>){{end}}:</p>
<ul>{{range .Summary.PastDeadline}}<li>{{.}}</li>{{end}}</ul>
{{if and .Authority .Authority.Notes}}<p class="disclaimer">{{.Authority.Notes}}</p>{{end}}
{{end}}

<h2>Per broker</h2>
{{range .Brokers}}
<div class="broker{{if .PastDeadline}} past-deadline{{end}}">
  <h3>{{.BrokerName}} {{if .PastDeadline}}<span class="flag">PAST DEADLINE</span>{{end}}</h3>
  <dl>
    {{if .BrokerEmail}}<dt>Contact</dt><dd>{{.BrokerEmail}}</dd>{{end}}
    {{if .BrokerWebsite}}<dt>Website</dt><dd>{{.BrokerWebsite}}</dd>{{end}}
    {{if .PipelineStatus}}<dt>Current stage</dt><dd>{{.PipelineStatus}}</dd>{{end}}
    {{if and .DeadlineDate (nonzero .DeadlineDate)}}<dt>Response deadline</dt><dd>{{date .DeadlineDate}}</dd>{{end}}
  </dl>

  {{range .Requests}}
  <div class="event {{.Status}}">
    <strong>Request sent{{if .Manual}} by hand{{end}}</strong> {{if nonzero .SentAt}}on {{datetime .SentAt}}{{end}} to {{.Recipient}}
    ({{.Status}}{{if .Manual}}, recorded manually{{end}}{{if .Error}}: {{.Error}}{{end}})<br>
    <span class="muted">Legal basis:</span> {{.LegalBasis}}<br>
    {{if .MessageID}}<span class="muted">Message-ID:</span> <code>{{.MessageID}}</code><br>{{end}}
    {{if .RenderedBody}}
    <details>
      <summary>Reconstructed email</summary>
      <p class="disclaimer">Reconstructed from the "{{.Template}}" template on record with the
      current profile details. The exact message transmitted at the time is not archived.</p>
      <p><strong>Subject:</strong> {{.RenderedSubject}}</p>
      <pre>{{.RenderedBody}}</pre>
    </details>
    {{end}}
  </div>
  {{end}}

  {{if .Responses}}
    {{range .Responses}}
    <div class="event response">
      <strong>Reply received</strong> {{if nonzero .ReceivedAt}}on {{datetime .ReceivedAt}}{{end}} -
      classified <strong>{{.Type}}</strong>{{if .NeedsReview}} <span class="muted">(low confidence {{pct .Confidence}}, manual review advised)</span>{{end}}<br>
      {{if .From}}<span class="muted">From:</span> {{.From}}<br>{{end}}
      {{if .Subject}}<span class="muted">Subject:</span> {{.Subject}}<br>{{end}}
      {{if .FormURL}}<span class="muted">Opt-out form:</span> {{.FormURL}}<br>{{end}}
      {{if .ConfirmURL}}<span class="muted">Confirmation link:</span> {{.ConfirmURL}}<br>{{end}}
      {{if .Body}}<details><summary>Reply text</summary><pre>{{.Body}}</pre></details>{{end}}
    </div>
    {{end}}
  {{else}}
    <div class="event"><em>No reply recorded.</em></div>
  {{end}}
</div>
{{end}}

<h2>Notes</h2>
<p class="disclaimer">This report is generated from Eraser's local history database
(<code>~/.eraser/history.db</code>). "Reply received" entries are Eraser's automatic
classification of inbound email and may not capture every message. Reconstructed
emails are regenerated from the request template, not retrieved from a sent-mail
archive. You are responsible for what you submit to any authority, and this report
is not legal advice.</p>
</body>
</html>
`
