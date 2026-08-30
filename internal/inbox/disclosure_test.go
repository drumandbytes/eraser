package inbox

import "testing"

// Article 15 replies must not be filed as ResponseSuccess. A deletion
// confirmation is finished business; a subject access disclosure still needs
// a human to read it, because it names the source the broker bought the data
// from and the recipients it sold it to - which is where the next set of
// brokers to chase comes from. Misfiling one as "success" buries it.
func TestClassifyDisclosureResponses(t *testing.T) {
	tests := []struct {
		name    string
		subject string
		body    string
	}{
		{
			name:    "portal link with article 15 reference",
			subject: "Re: Subject Access Request - Article 15 UK GDPR",
			body: `Dear Alex,

In response to your subject access request, please find attached a copy of the
personal data we hold about you. The source of the data was a licensed
electoral roll provider. The recipients to whom we have disclosed your data are
listed in Appendix B. Our retention period is 24 months.`,
		},
		{
			name:    "DSAR acronym with attachment",
			subject: "Your DSAR response is ready",
			body: `We have completed our review. Please find enclosed your personal
information report. Categories of personal data included: contact details,
property records, and inferred household income.`,
		},
		{
			name:    "download link phrasing",
			subject: "Data Subject Access Request",
			body: `You can download your data using the secure link below. This
contains the information we hold about you and the sources of the personal data.`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClassifyResponse(&Email{Subject: tt.subject, Body: tt.body, BrokerName: "Example"})
			if got.Type != ResponseDisclosure {
				t.Errorf("classified as %q, want %q (confidence %.2f, reason %q)",
					got.Type, ResponseDisclosure, got.Confidence, got.Reason)
			}
		})
	}
}

// Adding the disclosure category must not steal emails from the categories
// that already worked - particularly deletion confirmations, which share
// vocabulary ("your data", "request") with access responses.
func TestDisclosureDoesNotStealExistingClassifications(t *testing.T) {
	tests := []struct {
		name    string
		subject string
		body    string
		want    ResponseType
	}{
		{
			name:    "plain deletion confirmation stays success",
			subject: "Your removal request is complete",
			body:    "We have removed your data from our database. Your information has been deleted.",
			want:    ResponseSuccess,
		},
		{
			name:    "confirmation link stays confirmation_required",
			subject: "Please confirm your request",
			body:    "Click here to confirm your email address and complete your opt-out request.",
			want:    ResponseConfirmationRequired,
		},
		{
			name:    "no records found stays rejected",
			subject: "Re: your request",
			body:    "We do not have any records about you in our database. No matching records found.",
			want:    ResponseRejected,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClassifyResponse(&Email{Subject: tt.subject, Body: tt.body, BrokerName: "Example"})
			if got.Type != tt.want {
				t.Errorf("classified as %q, want %q (reason %q)", got.Type, tt.want, got.Reason)
			}
		})
	}
}
