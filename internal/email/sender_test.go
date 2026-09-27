package email

import "testing"

// digisamroc/eraser#6: From/To go straight into header lines, so a CRLF in a
// broker address could inject headers (e.g. Bcc). Guards validateMessage in Send.
func TestValidateEmail_RejectsHeaderInjection(t *testing.T) {
	tests := []struct {
		name    string
		email   string
		wantErr bool
	}{
		{"plain valid address", "broker@example.com", false},
		{"valid address with display name", "Broker Privacy <privacy@example.com>", false},
		{"CRLF injected Bcc header", "broker@example.com\r\nBcc: attacker@evil.com", true},
		{"bare LF injected header", "broker@example.com\nBcc: attacker@evil.com", true},
		{"bare CR", "broker@example.com\rBcc: attacker@evil.com", true},
		{"comma-smuggled second recipient", "broker@example.com,attacker@evil.com", true},
		{"semicolon-smuggled second recipient", "broker@example.com;attacker@evil.com", true},
		{"empty string", "", true},
		{"missing @", "not-an-email", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateEmail(tt.email)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateEmail(%q) = %v, want error=%v", tt.email, err, tt.wantErr)
			}
		})
	}
}

func TestValidateMessage_ChecksBothFromAndTo(t *testing.T) {
	bad := "ok@example.com\r\nBcc: attacker@evil.com"
	good := "ok@example.com"

	if err := validateMessage(Message{From: bad, To: good, Subject: "s", Body: "b"}); err == nil {
		t.Error("validateMessage did not reject an injected From address")
	}
	if err := validateMessage(Message{From: good, To: bad, Subject: "s", Body: "b"}); err == nil {
		t.Error("validateMessage did not reject an injected To address (the broker email path)")
	}
	if err := validateMessage(Message{From: good, To: good, Subject: "s", Body: "b"}); err != nil {
		t.Errorf("validateMessage rejected a valid message: %v", err)
	}
}
