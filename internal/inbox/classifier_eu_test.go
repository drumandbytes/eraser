package inbox

import "testing"

func TestGDPRAndMultilingualClassification(t *testing.T) {
	tests := []struct {
		name     string
		subject  string
		body     string
		expected ResponseType
	}{
		// GDPR English
		{
			name:     "erasure completed",
			body:     "We confirm that your erasure request has been actioned and all personal data concerning you has been erased from our systems.",
			expected: ResponseSuccess,
		},
		{
			name:     "right to be forgotten honoured",
			body:     "Your right to be forgotten request has been fulfilled.",
			expected: ResponseSuccess,
		},
		{
			name:     "no personal data concerning you",
			body:     "Following our checks, we hold no personal data concerning you and there is nothing to erase.",
			expected: ResponseRejected,
		},
		{
			name:     "manifestly unfounded",
			body:     "We consider this request to be manifestly unfounded and will not be actioning it.",
			expected: ResponseRejected,
		},
		{
			name:     "forwarded to DPO, one month",
			subject:  "Acknowledgement of your request",
			body:     "We acknowledge receipt of your request and have forwarded your request to our data protection officer. We will respond within one month.",
			expected: ResponsePending,
		},
		{
			name:     "identity verification required",
			body:     "Before we can process your request, please provide a copy of your government-issued ID.",
			expected: ResponseConfirmationRequired,
		},
		{
			name:     "GDPR portal",
			body:     "Please submit your request through our data protection portal.",
			expected: ResponseFormRequired,
		},
		// German
		{
			name:     "German - data deleted",
			subject:  "Ihre Anfrage",
			body:     "Sehr geehrte Damen und Herren, wir haben alle Ihre Daten gelöscht. Die Löschung wurde durchgeführt.",
			expected: ResponseSuccess,
		},
		{
			name:     "German - no data",
			body:     "Wir verarbeiten keine personenbezogenen Daten zu Ihnen. Sie sind nicht in unserer Datenbank.",
			expected: ResponseRejected,
		},
		{
			name:     "German - acknowledgement",
			subject:  "Eingangsbestätigung",
			body:     "Wir haben Ihre Anfrage erhalten und an unseren Datenschutzbeauftragten weitergeleitet. Eine Antwort erfolgt innerhalb eines Monats.",
			expected: ResponsePending,
		},
		{
			name:     "German - form",
			body:     "Bitte füllen Sie das Online-Formular aus, um Ihren Antrag zu stellen.",
			expected: ResponseFormRequired,
		},
		{
			name:     "German - ID copy",
			body:     "Zur Überprüfung Ihrer Identität benötigen wir eine Kopie Ihres Personalausweises.",
			expected: ResponseConfirmationRequired,
		},
		// French
		{
			name:     "French - data deleted",
			body:     "Bonjour, nous avons bien supprimé toutes vos données de nos systèmes.",
			expected: ResponseSuccess,
		},
		{
			name:     "French - no data",
			body:     "Nous ne détenons aucune donnée vous concernant dans nos fichiers.",
			expected: ResponseRejected,
		},
		{
			name:     "French - acknowledgement",
			subject:  "Accusé de réception",
			body:     "Nous accusons réception de votre demande. Une réponse vous sera apportée dans un délai d'un mois.",
			expected: ResponsePending,
		},
		{
			name:     "French - form",
			body:     "Veuillez utiliser notre formulaire dédié pour soumettre votre demande.",
			expected: ResponseFormRequired,
		},
		{
			name:     "French - identity",
			body:     "Afin de vérifier votre identité, merci de joindre une copie de votre pièce d'identité.",
			expected: ResponseConfirmationRequired,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			subject := tt.subject
			if subject == "" {
				subject = "Re: Erasure request under Article 17 GDPR"
			}
			result := ClassifyResponse(&Email{Subject: subject, Body: tt.body})
			if result.Type != tt.expected {
				t.Errorf("got %s, want %s (confidence %.2f, reason %q)", result.Type, tt.expected, result.Confidence, result.Reason)
			}
		})
	}
}

// The English CCPA-era test corpus must still classify the same way after the
// EU patterns were added (guard against a new pattern stealing a match).
func TestEUPatternsDoNotRegressEnglish(t *testing.T) {
	cases := []struct {
		body     string
		expected ResponseType
	}{
		{"please submit your request at https://example.com/dsar", ResponseFormRequired},
		{"we do not have any record of a report in our system", ResponseRejected},
		{"we have received your request. One of our Privacy Specialists will reach out", ResponsePending},
		{"Please click here to confirm your request", ResponseConfirmationRequired},
		{"your data has been removed and we no longer hold your information", ResponseSuccess},
	}
	for _, c := range cases {
		got := ClassifyResponse(&Email{Subject: "Re: Personal Data Removal Request", Body: c.body})
		if got.Type != c.expected {
			t.Errorf("%q: got %s, want %s", c.body, got.Type, c.expected)
		}
	}
}
