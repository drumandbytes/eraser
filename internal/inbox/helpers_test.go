package inbox

import (
	"path/filepath"
	"testing"

	"github.com/drumandbytes/eraser/internal/history"
)

func TestClassifyBySubjectOnly(t *testing.T) {
	cases := []struct {
		subject    string
		want       ResponseType
		review     bool
		confidence float64
	}{
		{"Your data deletion request has been completed", ResponseSuccess, false, 0.7},
		{"Hello there", ResponseUnknown, true, 0},
	}
	for _, c := range cases {
		typ, conf, review := ClassifyBySubjectOnly(c.subject)
		if typ != c.want || review != c.review || conf != c.confidence {
			t.Errorf("%q = %s %.1f %v, want %s %.1f %v", c.subject, typ, conf, review, c.want, c.confidence, c.review)
		}
	}
	// Every strong subject pattern family maps to its own type.
	for _, c := range []struct {
		subject string
		want    ResponseType
	}{
		{"We received your request - ticket #123", ResponsePending},
		{"Unable to process your request", ResponseRejected},
		{"Opt-out instructions for your request", ResponseFormRequired},
	} {
		if typ, _, _ := ClassifyBySubjectOnly(c.subject); typ != c.want {
			t.Errorf("%q = %s, want %s", c.subject, typ, c.want)
		}
	}
}

func TestSummarizeResponses(t *testing.T) {
	var in []ClassifiedResponse
	for _, typ := range []ResponseType{ResponseSuccess, ResponseFormRequired, ResponseConfirmationRequired,
		ResponseRejected, ResponsePending, ResponseBounced, ResponseUnknown, ResponseUnknown} {
		in = append(in, ClassifiedResponse{Type: typ, NeedsReview: typ == ResponseUnknown})
	}
	got := SummarizeResponses(in)
	want := Summary{Total: 8, Success: 1, FormRequired: 1, ConfirmRequired: 1, Rejected: 1, Pending: 1, Bounced: 1, Unknown: 2, NeedReview: 2}
	if got != want {
		t.Errorf("summary = %+v, want %+v", got, want)
	}
}

func TestPipelineStatusFor(t *testing.T) {
	for typ, want := range map[ResponseType]history.PipelineStatus{
		ResponseSuccess:              history.PipelineConfirmed,
		ResponseFormRequired:         history.PipelineFormRequired,
		ResponseConfirmationRequired: history.PipelineAwaitingConfirmation,
		ResponseRejected:             history.PipelineRejected,
		ResponsePending:              history.PipelineAwaitingResponse,
		ResponseUnknown:              history.PipelineAwaitingResponse,
	} {
		if got := pipelineStatusFor(typ); got != want {
			t.Errorf("%s -> %s, want %s", typ, got, want)
		}
	}
}

func TestExtractBouncedRecipientFromHTMLAndFallback(t *testing.T) {
	html := &Email{Subject: "Undeliverable", HTMLBody: "<p>Your message could not be delivered to:<br><b>dpo@gone.example</b></p>"}
	if got := ExtractBouncedRecipient(html); got != "dpo@gone.example" {
		t.Errorf("html bounce = %q", got)
	}
	// No phrasing to anchor on: the first non-system address wins.
	plain := &Email{Body: "From mailer-daemon@mx.example about privacy@vanished.example, reply to noreply@mx.example"}
	if got := ExtractBouncedRecipient(plain); got != "privacy@vanished.example" {
		t.Errorf("fallback = %q", got)
	}
	if got := ExtractBouncedRecipient(&Email{Body: "postmaster@mx.example only"}); got != "" {
		t.Errorf("only system addresses = %q", got)
	}
}

// RecordReply surfaces store errors rather than dropping a reply silently.
func TestRecordReplyStoreErrors(t *testing.T) {
	store, err := history.NewStore(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	_ = store.Close()
	e := &Email{BrokerID: "acme", Subject: "Re: x", HTMLBody: "<p>We deleted your data.</p>"}
	if _, outcome, err := RecordReply(store, e, true); err == nil || outcome != ReplySeen {
		t.Errorf("reclassify on a closed store = %v, %v", outcome, err)
	}
	if _, outcome, err := RecordReply(store, e, false); err == nil || outcome != ReplySeen {
		t.Errorf("insert on a closed store = %v, %v", outcome, err)
	}
}

// A reply stored without a body gets the body filled in on reclassify.
func TestRecordReplyBackfillsBody(t *testing.T) {
	store, err := history.NewStore(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	if err := store.AddBrokerResponse(&history.BrokerResponse{BrokerID: "acme", BrokerName: "Acme", ResponseType: "unknown", EmailSubject: "Re: x"}); err != nil {
		t.Fatal(err)
	}
	e := &Email{BrokerID: "acme", Subject: "Re: x", HTMLBody: "<p>We deleted your data.</p>"}
	if _, outcome, err := RecordReply(store, e, true); err != nil || outcome != ReplyUpdated {
		t.Fatalf("outcome %v, err %v", outcome, err)
	}
	all, _ := store.GetAllBrokerResponses()
	if len(all) != 1 || all[0].EmailBody == "" {
		t.Errorf("body not backfilled: %+v", all)
	}
}

func TestIsTrackingURL(t *testing.T) {
	for url, want := range map[string]bool{
		"https://broker.example/open.gif":        true,
		"https://broker.example/pixel/a.png":     true,
		"https://broker.example/logo.png":        false,
		"https://broker.example/privacy/opt-out": false,
	} {
		if got := isTrackingURL(url); got != want {
			t.Errorf("isTrackingURL(%q) = %v", url, got)
		}
	}
}
