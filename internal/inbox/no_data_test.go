package inbox

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/drumandbytes/eraser/internal/history"
)

func TestNoDataClassification(t *testing.T) {
	for _, tc := range []struct {
		name, subject, body, html string
		kind                      ResponseType
		review                    bool
	}{
		{"clear", "Re: request", "We do not have any records of you in our database.", "", ResponseNoData, false},
		{"no records of you", "Re: request", "We have no records of you in our database.", "", ResponseNoData, false},
		{"no data about you", "Re: request", "We hold no data about you.", "", ResponseNoData, false},
		{"courtesy", "Ticket #123456 received", "We hold no personal data concerning you. If you have questions, contact our team.", "", ResponseNoData, false},
		{"html", "Re: request", "", "<p>We have no data linked to your email.</p>", ResponseNoData, false},
		{"subject", "No matching records found", "", "", ResponseNoData, false},
		{"german", "Ihre Anfrage", "Wir speichern keine personenbezogenen Daten über Ihnen.", "", ResponseNoData, false},
		{"french", "Votre demande", "Nous ne détenons aucune donnée vous concernant.", "", ResponseNoData, false},
		{"conditional", "Re: request", "If no records found, we will tell you.", "", ResponseNoData, true},
		{"subject echoed by acknowledgment", "Re: No records found", "We have received your request and will get back to you.", "", ResponseNoData, true},
		{"conflicting subject", "Request completed", "We have no data linked to your email.", "", ResponseNoData, true},
		{"verification", "Re: request", "No matching records found. Please verify your identity before we can proceed.", "", ResponseNoData, true},
		{"mixed refusal", "Re: request", "We hold no personal data concerning you. Your request has been rejected.", "", ResponseNoData, true},
		{"uncertain subject", "Unable to locate your data", "", "", ResponseNoData, true},
		{"deletion", "Re: request", "We have deleted your data and no longer hold your information.", "", ResponseSuccess, false},
		{"refusal", "Request denied", "We cannot process your request.", "", ResponseRejected, false},
		{"quoted request", "Re: request", "We have received your request.\nOn Monday you wrote:\nNo records found", "", ResponsePending, false},
		{"quoted html", "Re: request", "", "<p>We have received your request.</p><blockquote>Please confirm no records found</blockquote>", ResponsePending, false},
		{"bounce", "Delivery status notification (failure)", "Your message could not be delivered. No records found.", "", ResponseBounced, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := ClassifyResponse(&Email{Subject: tc.subject, Body: tc.body, HTMLBody: tc.html})
			if got.Type != tc.kind || got.NeedsReview != tc.review {
				t.Fatalf("got %s review=%v confidence=%v, want %s review=%v", got.Type, got.NeedsReview, got.Confidence, tc.kind, tc.review)
			}
		})
	}
	if kind, confidence, review := ClassifyBySubjectOnly("No matching records found"); kind != ResponseNoData || confidence < 0.7 || review {
		t.Fatal("clear subject-only classification missing")
	}
}

func TestNoDataNewReplyResolvesOnlyConfidentCurrentOutcome(t *testing.T) {
	for _, review := range []bool{false, true} {
		t.Run(map[bool]string{false: "clear", true: "ambiguous"}[review], func(t *testing.T) {
			store, err := history.NewStore(filepath.Join(t.TempDir(), "history.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			if err := store.Add(&history.Record{BrokerID: "acme", BrokerName: "Acme", Status: history.StatusSent, SentAt: time.Now().Add(-time.Hour)}); err != nil {
				t.Fatal(err)
			}
			body := "We do not have any records of you."
			if review {
				body += " Please verify your identity."
			}
			_, _, err = RecordReply(store, &Email{BrokerID: "acme", BrokerName: "Acme", Subject: "Our response", Body: body, ReceivedAt: time.Now()}, false)
			if err != nil {
				t.Fatal(err)
			}
			requests, _ := store.LatestRequests("default")
			want := history.PipelineNoData
			if review {
				want = history.PipelineEmailSent
			}
			if requests[0].PipelineStatus != want {
				t.Fatalf("pipeline=%s want=%s", requests[0].PipelineStatus, want)
			}
		})
	}
}

func TestNoDataReviewKeepsActionLinks(t *testing.T) {
	got := ClassifyResponse(&Email{Subject: "Your privacy request", Body: "No records found. Please verify your identity: https://example.com/confirm?token=123"})
	if got.Type != ResponseNoData || !got.NeedsReview || got.ConfirmURL == "" {
		t.Fatalf("review should retain verification link: %+v", got)
	}
}
