package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/drumandbytes/eraser/internal/history"
)

func TestResponsesAPIErrorsAndReviewFilter(t *testing.T) {
	nilStore := nilStoreServer(t)
	rec := httptest.NewRecorder()
	nilStore.handleAPIResponses(rec, httptest.NewRequest(http.MethodGet, "/api/pipeline/responses", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("nil store: status=%d", rec.Code)
	}

	s := pipelineTestServer(t)
	pipelineReply(t, s, history.BrokerResponse{BrokerID: "flagged", BrokerName: "Flagged", ResponseType: "no_data_held", NeedsReview: true})
	pipelineReply(t, s, history.BrokerResponse{BrokerID: "plain", BrokerName: "Plain", ResponseType: "no_data_held"})
	rec = httptest.NewRecorder()
	s.handleAPIResponses(rec, httptest.NewRequest(http.MethodGet, "/api/pipeline/responses?needs_review=true", nil))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "Flagged") || strings.Contains(rec.Body.String(), "Plain") {
		t.Fatalf("needs_review filter: status=%d body=%s", rec.Code, rec.Body.String())
	}

	broken := pipelineTestServer(t)
	_ = broken.historyStore.Close()
	rec = httptest.NewRecorder()
	broken.handleAPIResponses(rec, httptest.NewRequest(http.MethodGet, "/api/pipeline/responses", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("closed store: status=%d", rec.Code)
	}
}

func TestReclassifyCountsEveryOutcome(t *testing.T) {
	s := withInbox(t, imapServer(t))
	for _, r := range []history.BrokerResponse{
		{BrokerID: "nodata", BrokerName: "No data", EmailSubject: "Re: request", EmailBody: "We do not have any records of you in our database."},
		{BrokerID: "refused", BrokerName: "Refused", EmailSubject: "Request denied", EmailBody: "We cannot process your request."},
		{BrokerID: "pending", BrokerName: "Pending", EmailSubject: "Re: request", EmailBody: "We have received your request and will get back to you."},
		{BrokerID: "form", BrokerName: "Form", EmailSubject: "Re: request", EmailBody: "Please use our web form at https://example.com/optout to submit your request."},
	} {
		r.ProfileID = "default"
		r.ResponseType = "unknown"
		if err := s.historyStore.AddBrokerResponse(&r); err != nil {
			t.Fatal(err)
		}
	}
	body := do(t, s, http.MethodPost, "/api/inbox/reclassify", true).Body.String()
	if !strings.Contains(body, "Reclassification complete") {
		t.Fatalf("reclassify: %s", body)
	}
	for _, id := range []string{"nodata", "refused", "pending", "form"} {
		resp := history.BrokerResponse{}
		all, _, err := s.historyStore.QueryBrokerResponses("default", history.ResponseQuery{Search: id, Limit: 10})
		if err != nil || len(all) != 1 {
			t.Fatalf("%s: rows=%d err=%v", id, len(all), err)
		}
		resp = all[0]
		if resp.ResponseType == "unknown" {
			t.Errorf("%s stayed unknown", id)
		}
	}
}
