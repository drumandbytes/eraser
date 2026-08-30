package template

import (
	"strings"
	"testing"

	"github.com/eraser-privacy/eraser/internal/broker"
	"github.com/eraser-privacy/eraser/internal/config"
	"github.com/eraser-privacy/eraser/internal/history"
)

func ukProfile() config.Profile {
	return config.Profile{
		FirstName: "Alex",
		LastName:  "Fenwick",
		Email:     "alex@example.co.uk",
		Address:   "12 Peveril Street",
		City:      "Nottingham",
		ZipCode:   "NG7 1AB",
		Country:   "United Kingdom",
	}
}

// The UK templates must cite UK GDPR and the ICO, not EU GDPR and a generic
// "supervisory authority". The pre-existing gdpr template in this fork was
// written for an EU (Latvia) resident, and the distinction is substantive:
// a UK resident's regulator is the ICO and their statutory deadline is a
// calendar month rather than "30 days".
func TestUKTemplatesCiteUKLawAndICO(t *testing.T) {
	e, err := NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	b := broker.Broker{Name: "Example Broker"}

	for _, name := range []string{"uk-access", "uk-erasure", "uk-combined"} {
		t.Run(name, func(t *testing.T) {
			email, err := e.Render(name, ukProfile(), b)
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{
				"UK General Data Protection Regulation",
				"Data Protection Act 2018",
				"Information Commissioner's Office",
				"within one month",
				"Alex Fenwick",
				"Example Broker",
			} {
				if !strings.Contains(email.Body, want) {
					t.Errorf("%s: missing %q", name, want)
				}
			}
			// "30 days" is the EU-template phrasing and is wrong for the UK,
			// where the deadline is one calendar month.
			if strings.Contains(email.Body, "within 30 days") {
				t.Errorf("%s: says 'within 30 days'; UK GDPR's deadline is one calendar month", name)
			}
			if email.Subject == "" {
				t.Errorf("%s: empty subject", name)
			}
		})
	}
}

// The access template's value is the specific Article 15 sub-paragraphs it
// invokes. 15(1)(g) (the source they bought your data from) and 15(1)(c)
// (the recipients they sold it to) are the two that turn one reply into the
// next set of brokers to chase, so they must not quietly go missing.
func TestUKAccessTemplateAsksForSourcesAndRecipients(t *testing.T) {
	e, err := NewEngine()
	if err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"uk-access", "uk-combined"} {
		email, err := e.Render(name, ukProfile(), broker.Broker{Name: "Example Broker"})
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{
			"Article 15(1)(c)", // recipients
			"Article 15(1)(g)", // source
			"Article 15(1)(h)", // automated decision-making / profiling
			"Article 15(3)",    // copy of the data itself
		} {
			if !strings.Contains(email.Body, want) {
				t.Errorf("%s: missing %s", name, want)
			}
		}
	}
}

// The combined template exists to get both rights into one email without the
// erasure destroying the access response. If the instruction to answer the
// access request first is lost, the template is actively counterproductive:
// the broker deletes the record and truthfully replies that it holds nothing.
func TestUKCombinedTemplateSequencesAccessBeforeErasure(t *testing.T) {
	e, err := NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	email, err := e.Render("uk-combined", ukProfile(), broker.Broker{Name: "Example Broker"})
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(email.Body, "BEFORE carrying out the erasure") {
		t.Error("combined template must tell the broker to answer the access request before erasing")
	}

	accessIdx := strings.Index(email.Body, "PART A")
	erasureIdx := strings.Index(email.Body, "PART B")
	if accessIdx < 0 || erasureIdx < 0 {
		t.Fatal("combined template should have both PART A and PART B sections")
	}
	if accessIdx > erasureIdx {
		t.Error("the access request should appear before the erasure request")
	}
}

func TestRequestTypeFor(t *testing.T) {
	cases := map[string]string{
		"uk-access":   history.RequestAccess,
		"uk-combined": history.RequestCombined,
		"uk-erasure":  history.RequestErasure,
		"gdpr":        history.RequestErasure,
		"ccpa":        history.RequestErasure,
		"generic":     history.RequestErasure,
		// An unknown/custom template must fall back to erasure rather than
		// inventing a fourth category that would get its own cooldown slot.
		"something-custom": history.RequestErasure,
	}
	for name, want := range cases {
		if got := RequestTypeFor(name); got != want {
			t.Errorf("RequestTypeFor(%q) = %q, want %q", name, got, want)
		}
	}
}

// Every name TemplateNames advertises (it drives --template's help text and
// validation) must actually be loadable, or the CLI will offer a template
// that fails at send time.
func TestTemplateNamesAllRenderable(t *testing.T) {
	e, err := NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range TemplateNames() {
		if !e.IsKnownTemplate(name) {
			t.Errorf("TemplateNames lists %q but the engine did not load it", name)
			continue
		}
		if _, err := e.Render(name, ukProfile(), broker.Broker{Name: "Example Broker"}); err != nil {
			t.Errorf("Render(%q): %v", name, err)
		}
	}
}
