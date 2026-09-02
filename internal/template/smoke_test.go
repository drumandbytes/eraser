package template

import (
	"strings"
	"testing"

	"github.com/drumandbytes/eraser/internal/broker"
	"github.com/drumandbytes/eraser/internal/config"
)

func TestRenderIncludesNameAndEmailVariants(t *testing.T) {
	e, err := NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	profile := config.Profile{
		FirstName:         "Māris",
		LastName:          "Popens",
		Email:             "maris@popens.lv",
		AdditionalEmails:  []string{"old.maris@example.com", "maris.work@example.com"},
		NameVariants:      []string{"Maris Popens"},
		PreviousAddresses: []string{"Tartu mnt 15, Tallinn, 10117", "Duntes iela 23, Riga, LV-1005"},
	}
	b := broker.Broker{Name: "Example Broker"}

	email, err := e.Render("gdpr", profile, b)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Māris Popens", "Also Known As: Maris Popens",
		"Other Email Addresses Used: old.maris@example.com, maris.work@example.com",
		"Previously Lived At: Tartu mnt 15, Tallinn, 10117; Duntes iela 23, Riga, LV-1005"} {
		if !strings.Contains(email.Body, want) {
			t.Errorf("rendered email missing %q\n---\n%s", want, email.Body)
		}
	}
}

func TestPreviousAddressesRenderInAllTemplates(t *testing.T) {
	e, err := NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	profile := config.Profile{
		FirstName:         "Māris",
		LastName:          "Popens",
		Email:             "maris@popens.lv",
		PreviousAddresses: []string{"Tartu mnt 15, Tallinn, 10117"},
	}
	b := broker.Broker{Name: "Example Broker"}

	for _, tmplName := range []string{"gdpr", "ccpa", "generic"} {
		email, err := e.Render(tmplName, profile, b)
		if err != nil {
			t.Fatalf("%s: %v", tmplName, err)
		}
		if !strings.Contains(email.Body, "Previously Lived At: Tartu mnt 15, Tallinn, 10117") {
			t.Errorf("%s template missing previous address line\n---\n%s", tmplName, email.Body)
		}
	}

	// And confirm it's omitted entirely when there's nothing to show.
	emptyProfile := config.Profile{FirstName: "Māris", LastName: "Popens", Email: "maris@popens.lv"}
	email, err := e.Render("gdpr", emptyProfile, b)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(email.Body, "Previously Lived At") {
		t.Errorf("expected no 'Previously Lived At' line when PreviousAddresses is empty\n---\n%s", email.Body)
	}
}
