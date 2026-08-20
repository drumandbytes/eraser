package email

import "testing"

// Regression coverage for a real finding filed against upstream
// (digisamroc/eraser#6): SMTP header injection via unsanitized From/To
// fields. smtp.go interpolates msg.From/msg.To directly into raw header
// lines ("To: %s\r\n"), so if a broker's email in data/brokers.yaml ever
// contained a CRLF, an attacker could inject arbitrary headers (e.g. a
// forged Bcc) into every message sent to that address. validateMessage
// (called first thing in Send) is what actually prevents this - this test
// exists so a future refactor can't silently drop that call.
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
