package template

import (
	"testing"

	"github.com/drumandbytes/eraser/internal/broker"
	"github.com/drumandbytes/eraser/internal/config"
)

func TestRenderSubjectsAndUnknownTemplate(t *testing.T) {
	e, err := NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	p := config.Profile{FirstName: "Jane", LastName: "Doe", Email: "jane@example.com"}
	b := broker.Broker{Name: "Acme"}
	for name, want := range map[string]string{
		"gdpr":    "GDPR Data Erasure Request - Article 17 Right to Erasure",
		"ccpa":    "CCPA Data Deletion Request - Right to Delete Personal Information",
		"generic": "Personal Data Removal Request",
	} {
		email, err := e.Render(name, p, b)
		if err != nil || email.Subject != want || email.Body == "" {
			t.Errorf("%s: %+v, %v", name, email, err)
		}
	}
	if _, err := e.Render("nope", p, b); err == nil {
		t.Error("unknown template rendered")
	}
}
