package template

import (
	"strings"
	"testing"

	"github.com/eraser-privacy/eraser/internal/broker"
	"github.com/eraser-privacy/eraser/internal/config"
)

func TestRenderIncludesNameAndEmailVariants(t *testing.T) {
	e, err := NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	profile := config.Profile{
		FirstName:        "Māris",
		LastName:         "Popens",
		Email:            "maris@popens.lv",
		AdditionalEmails: []string{"old.maris@example.com", "maris.work@example.com"},
		NameVariants:     []string{"Maris Popens"},
	}
	b := broker.Broker{Name: "Example Broker"}

	email, err := e.Render("gdpr", profile, b)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Māris Popens", "Also Known As: Maris Popens",
		"Other Email Addresses Used: old.maris@example.com, maris.work@example.com"} {
		if !strings.Contains(email.Body, want) {
			t.Errorf("rendered email missing %q\n---\n%s", want, email.Body)
		}
	}
}
