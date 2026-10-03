package evidence

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/drumandbytes/eraser/internal/broker"
	"github.com/drumandbytes/eraser/internal/config"
	"github.com/drumandbytes/eraser/internal/history"
	emailtmpl "github.com/drumandbytes/eraser/internal/template"
)

func testProfile() config.Profile {
	return config.Profile{
		FirstName:    "Jane",
		LastName:     "Doe",
		Email:        "jane@example.com",
		Address:      "1 Main St",
		City:         "Riga",
		Country:      "Latvia",
		DateOfBirth:  "1990-01-01",
		NameVariants: []string{"J. Doe"},
	}
}

func testBrokerDB() *broker.BrokerDatabase {
	return &broker.BrokerDatabase{Brokers: []broker.Broker{
		{ID: "acme", Name: "Acme Data", Email: "privacy@acme.example", Website: "https://acme.example", Region: "us", Category: "people-search"},
		{ID: "globex", Name: "Globex", Email: "dpo@globex.example", Region: "eu", Category: "marketing"},
	}}
}

func testEngine(t *testing.T) *emailtmpl.Engine {
	t.Helper()
	engine, err := emailtmpl.NewEngine()
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	return engine
}

func findBroker(t *testing.T, rep EvidenceReport, id string) *BrokerEvidence {
	t.Helper()
	for i := range rep.Brokers {
		if rep.Brokers[i].BrokerID == id {
			return &rep.Brokers[i]
		}
	}
	t.Fatalf("no evidence for broker %q", id)
	return nil
}

func TestBuildEvidenceReport(t *testing.T) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)

	requests := []history.Record{
		{ // sent 3 months ago, never answered -> past deadline
			BrokerID: "acme", BrokerName: "Acme Data", Email: "privacy@acme.example",
			Template: "gdpr", Status: history.StatusSent,
			SentAt: now.AddDate(0, -3, 0), MessageID: "<abc@mail>",
			PipelineStatus: history.PipelineAwaitingResponse,
		},
		{ // sent 3 days ago, answered -> not past deadline
			BrokerID: "globex", BrokerName: "Globex", Email: "dpo@globex.example",
			Template: "gdpr", Status: history.StatusSent, SentAt: now.AddDate(0, 0, -3),
		},
	}
	responses := []history.BrokerResponse{
		{BrokerID: "globex", BrokerName: "Globex", ResponseType: "success",
			EmailFrom: "dpo@globex.example", EmailSubject: "Done", ReceivedAt: now.AddDate(0, 0, -1)},
	}

	rep := Build(testProfile(), requests, responses, testBrokerDB(), testEngine(t), time.Time{}, now)

	if rep.Subject.FullName != "Jane Doe" {
		t.Errorf("subject name = %q", rep.Subject.FullName)
	}
	if rep.Subject.Address != "1 Main St, Riga, Latvia" {
		t.Errorf("subject address = %q", rep.Subject.Address)
	}
	if rep.Authority == nil || rep.Authority.Code != "LV" {
		t.Errorf("authority = %+v, want Latvia's", rep.Authority)
	}
	if rep.Summary.Sent != 2 || rep.Summary.BrokersContacted != 2 || rep.Summary.BrokersResponded != 1 {
		t.Errorf("summary = %+v", rep.Summary)
	}
	if len(rep.Summary.PastDeadline) != 1 || rep.Summary.PastDeadline[0] != "Acme Data" {
		t.Errorf("past deadline = %v, want [Acme Data]", rep.Summary.PastDeadline)
	}

	acme, globex := findBroker(t, rep, "acme"), findBroker(t, rep, "globex")
	if !acme.PastDeadline {
		t.Error("acme should be past deadline")
	}
	if acme.PipelineStatus != string(history.PipelineAwaitingResponse) {
		t.Errorf("acme pipeline = %q", acme.PipelineStatus)
	}
	if acme.BrokerWebsite != "https://acme.example" {
		t.Errorf("acme website = %q", acme.BrokerWebsite)
	}
	if globex.PastDeadline {
		t.Error("globex should not be past deadline (it responded)")
	}
	if len(acme.Requests) != 1 || acme.Requests[0].MessageID != "<abc@mail>" {
		t.Errorf("acme request = %+v", acme.Requests)
	}
	if !strings.Contains(acme.Requests[0].LegalBasis, "Article 17") {
		t.Errorf("legal basis = %q", acme.Requests[0].LegalBasis)
	}
	if !strings.Contains(acme.Requests[0].RenderedBody, "erasure") {
		t.Errorf("reconstructed body missing expected text: %q", acme.Requests[0].RenderedBody)
	}
	if acme.Requests[0].RenderedSubject == "" {
		t.Error("reconstructed subject empty")
	}
}

func TestBuildEvidenceReportSinceFilter(t *testing.T) {
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	requests := []history.Record{
		{BrokerID: "acme", Template: "gdpr", Status: history.StatusSent, SentAt: now.AddDate(0, -6, 0)},
		{BrokerID: "globex", Template: "gdpr", Status: history.StatusSent, SentAt: now.AddDate(0, 0, -2)},
	}
	rep := Build(testProfile(), requests, nil, testBrokerDB(), testEngine(t), now.AddDate(0, -1, 0), now)
	if rep.Summary.TotalRequests != 1 || rep.Summary.BrokersContacted != 1 {
		t.Errorf("since filter not applied: %+v", rep.Summary)
	}
	if rep.Brokers[0].BrokerID != "globex" {
		t.Errorf("wrong broker survived filter: %s", rep.Brokers[0].BrokerID)
	}
}

func TestBuildUnknownBrokerFallsBackToRecordName(t *testing.T) {
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	requests := []history.Record{
		{BrokerID: "gone", BrokerName: "Gone Ltd", Email: "x@gone.example", Template: "gdpr",
			Status: history.StatusSent, SentAt: now.AddDate(0, 0, -1), SentMethod: "manual"},
	}
	rep := Build(testProfile(), requests, nil, testBrokerDB(), testEngine(t), time.Time{}, now)
	gone := findBroker(t, rep, "gone")
	if gone.BrokerName != "Gone Ltd" {
		t.Errorf("name = %q, want the history record's name", gone.BrokerName)
	}
	if gone.BrokerEmail != "" || gone.Requests[0].RenderedBody != "" {
		t.Error("a broker missing from the list has no contact or reconstructed email")
	}
	if !gone.Requests[0].Manual {
		t.Error("manual send not flagged")
	}
}

// The deadline runs from the earliest successful send, and failed sends
// count in the summary but never start the clock.
func TestBuildDeadlineAndCounts(t *testing.T) {
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	first := now.AddDate(0, 0, -50)
	requests := []history.Record{
		{BrokerID: "acme", Template: "ccpa", Status: history.StatusFailed, SentAt: now.AddDate(0, 0, -60), Error: "smtp 550"},
		{BrokerID: "acme", Template: "ccpa", Status: history.StatusSent, SentAt: now.AddDate(0, 0, -10),
			PipelineStatus: history.PipelineEmailSent},
		{BrokerID: "acme", Template: "ccpa", Status: history.StatusSent, SentAt: first},
		{BrokerID: "globex", Template: "generic", Status: history.StatusFailed, SentAt: now.AddDate(0, 0, -5)},
	}
	rep := Build(testProfile(), requests, nil, testBrokerDB(), testEngine(t), time.Time{}, now)

	if rep.Summary.TotalRequests != 4 || rep.Summary.Sent != 2 || rep.Summary.Failed != 2 {
		t.Errorf("summary = %+v", rep.Summary)
	}
	acme := findBroker(t, rep, "acme")
	if want := first.AddDate(0, 0, 45); acme.DeadlineDate == nil || !acme.DeadlineDate.Equal(want) {
		t.Errorf("acme deadline = %v, want %v (45 days from the earliest send)", acme.DeadlineDate, want)
	}
	if !acme.PastDeadline {
		t.Error("acme is 50 days past a 45-day deadline")
	}
	if acme.Requests[0].Error != "smtp 550" {
		t.Errorf("failed request error = %q", acme.Requests[0].Error)
	}
	if globex := findBroker(t, rep, "globex"); globex.DeadlineDate != nil || globex.PastDeadline {
		t.Error("a broker never successfully emailed has no deadline")
	}
}

// Bounces and noise don't stop the clock; only a substantive reply does.
func TestBuildNonSubstantiveReplyStaysPastDeadline(t *testing.T) {
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	requests := []history.Record{
		{BrokerID: "acme", BrokerName: "Acme Data", Template: "gdpr", Status: history.StatusSent, SentAt: now.AddDate(0, -2, 0)},
		{BrokerID: "acme", BrokerName: "Acme Data", Template: "gdpr", Status: history.StatusSent, SentAt: now.AddDate(0, -2, 1)},
	}
	responses := []history.BrokerResponse{
		{BrokerID: "acme", ResponseType: "unknown", ReceivedAt: now.AddDate(0, 0, -1)},
		{BrokerID: "acme", ResponseType: "bounced", ReceivedAt: now.AddDate(0, -1, 0)},
	}
	rep := Build(testProfile(), requests, responses, testBrokerDB(), testEngine(t), time.Time{}, now)
	acme := findBroker(t, rep, "acme")
	if !acme.PastDeadline {
		t.Error("non-substantive replies must not clear the past-deadline flag")
	}
	if len(rep.Summary.PastDeadline) != 1 {
		t.Errorf("past deadline listed %d times, want once", len(rep.Summary.PastDeadline))
	}
	if acme.Responses[0].Type != "bounced" || acme.Responses[1].Type != "unknown" {
		t.Errorf("responses not sorted oldest first: %+v", acme.Responses)
	}
	if rep.Summary.BrokersResponded != 1 {
		t.Errorf("brokers responded = %d", rep.Summary.BrokersResponded)
	}
}

func TestLegalBasisAndDeadline(t *testing.T) {
	sent := time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		tmpl, basis string
		deadline    time.Time
	}{
		{"gdpr", "Article 17", sent.AddDate(0, 1, 0)},
		{"ccpa", "1798.105", sent.AddDate(0, 0, 45)},
		{"generic", "30 days", sent.AddDate(0, 0, 30)},
	}
	for _, c := range cases {
		if got := legalBasisFor(c.tmpl); !strings.Contains(got, c.basis) {
			t.Errorf("legalBasisFor(%q) = %q, want it to mention %q", c.tmpl, got, c.basis)
		}
		if got := deadlineFor(c.tmpl, sent); !got.Equal(c.deadline) {
			t.Errorf("deadlineFor(%q) = %v, want %v", c.tmpl, got, c.deadline)
		}
	}
}

func TestJoinAddressSkipsBlankParts(t *testing.T) {
	p := config.Profile{Address: "1 Main St", City: "  ", ZipCode: "LV-1010", Country: "Latvia"}
	if got := joinAddress(p); got != "1 Main St, LV-1010, Latvia" {
		t.Errorf("joinAddress = %q", got)
	}
	if got := joinAddress(config.Profile{}); got != "" {
		t.Errorf("empty profile address = %q", got)
	}
}

func TestRenderHTML(t *testing.T) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	p := testProfile()
	p.Phone = "+371 2000 0000"
	p.AdditionalPhones = []string{"+371 2000 0001"}
	p.AdditionalEmails = []string{"jane.alt@example.com"}
	p.PreviousAddresses = []string{"2 Old Rd, Jurmala"}
	requests := []history.Record{
		{BrokerID: "acme", BrokerName: "Acme Data", Email: "privacy@acme.example", Template: "gdpr",
			Status: history.StatusSent, SentAt: now.AddDate(0, -3, 0), MessageID: "<abc@mail>", SentMethod: "manual",
			PipelineStatus: history.PipelineAwaitingResponse},
		{BrokerID: "globex", BrokerName: "Globex", Email: "dpo@globex.example", Template: "gdpr",
			Status: history.StatusSent, SentAt: now.AddDate(0, 0, -3)},
	}
	responses := []history.BrokerResponse{
		{BrokerID: "globex", ResponseType: "form_required", EmailFrom: "dpo@globex.example", EmailSubject: "Use our form",
			EmailBody: "<script>alert(1)</script>", FormURL: "https://globex.example/optout",
			ConfirmURL: "https://globex.example/confirm", Confidence: 0.42, NeedsReview: true, ReceivedAt: now.AddDate(0, 0, -1)},
	}
	rep := Build(p, requests, responses, testBrokerDB(), testEngine(t), time.Time{}, now)
	rep.ProfileID = "jane"

	out, err := RenderHTML(rep)
	if err != nil {
		t.Fatalf("RenderHTML: %v", err)
	}
	html := string(out)
	for _, want := range []string{
		"Jane Doe", "also J. Doe", "jane.alt@example.com", "2 Old Rd, Jurmala", "1990-01-01",
		"2000 0001", "profile <code>jane</code>",
		"PAST DEADLINE", "Request sent by hand", "recorded manually", "&lt;abc@mail&gt;",
		"Reconstructed email", "awaiting_response", "Response deadline",
		"classified <strong>form_required</strong>", "low confidence 42%",
		"https://globex.example/optout", "https://globex.example/confirm",
		rep.Authority.Website, // complaint pointer for the past-deadline broker
	} {
		if !strings.Contains(html, want) {
			t.Errorf("html missing %q", want)
		}
	}
	if strings.Contains(html, "<script>alert(1)</script>") {
		t.Error("reply body was not HTML-escaped")
	}
}

func TestRenderHTMLNoReplies(t *testing.T) {
	rep := Build(config.Profile{FirstName: "A", Email: "a@example.com"},
		[]history.Record{{BrokerID: "acme", Template: "gdpr", Status: history.StatusFailed}},
		nil, testBrokerDB(), testEngine(t), time.Time{}, time.Now())
	out, err := RenderHTML(rep)
	if err != nil {
		t.Fatalf("RenderHTML: %v", err)
	}
	if !strings.Contains(string(out), "No reply recorded.") {
		t.Error("missing no-reply placeholder")
	}
	if strings.Contains(string(out), "Date of birth") {
		t.Error("blank optional fields should be omitted")
	}
}

// Empty lists marshal as [] - export consumers rely on it (docs/stability.md).
func TestBuildJSONHasNoNullLists(t *testing.T) {
	rep := Build(testProfile(),
		[]history.Record{{BrokerID: "acme", Template: "gdpr", Status: history.StatusSent, SentAt: time.Now()}},
		nil, testBrokerDB(), testEngine(t), time.Time{}, time.Now())
	data, err := json.Marshal(rep.Brokers)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "null") {
		t.Errorf("broker evidence JSON contains null: %s", data)
	}
}
