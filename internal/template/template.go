package template

import (
	"bytes"
	"embed"
	"fmt"
	"strings"
	"text/template"
	"time"

	"github.com/eraser-privacy/eraser/internal/broker"
	"github.com/eraser-privacy/eraser/internal/config"
	"github.com/eraser-privacy/eraser/internal/history"
)

//go:embed templates/*.tmpl
var embeddedTemplates embed.FS

// EmailData contains all data available to email templates
type EmailData struct {
	// User profile
	FirstName      string
	LastName       string
	FullName       string
	Email          string
	OtherEmails    string // comma-separated additional emails, empty if none
	OtherNames     string // comma-separated name variants, empty if none
	OtherAddresses string // semicolon-separated previous addresses, empty if none
	Address        string
	City           string
	State          string
	ZipCode        string
	Country        string
	Phone          string
	OtherPhones    string // comma-separated additional phone numbers, empty if none
	DateOfBirth    string

	// Broker info
	BrokerName    string
	BrokerEmail   string
	BrokerWebsite string
	BrokerOptOut  string

	// Metadata
	Date     string
	Year     int
	Month    string
	Template string
}

// Email represents a rendered email ready to send
type Email struct {
	Subject string
	Body    string
}

// Engine handles email template rendering
type Engine struct {
	templates map[string]*template.Template
}

// NewEngine creates a new template engine
func NewEngine() (*Engine, error) {
	e := &Engine{
		templates: make(map[string]*template.Template),
	}

	templateNames := []string{"gdpr", "ccpa", "generic", "uk-access", "uk-erasure", "uk-combined"}
	for _, name := range templateNames {
		content, err := embeddedTemplates.ReadFile("templates/" + name + ".tmpl")
		if err != nil {
			return nil, fmt.Errorf("failed to read embedded template %s: %w", name, err)
		}

		tmpl, err := template.New(name).Parse(string(content))
		if err != nil {
			return nil, fmt.Errorf("failed to parse template %s: %w", name, err)
		}

		e.templates[name] = tmpl
	}

	return e, nil
}

// Render generates an email from a template
func (e *Engine) Render(templateName string, profile config.Profile, b broker.Broker) (*Email, error) {
	tmpl, ok := e.templates[templateName]
	if !ok {
		return nil, fmt.Errorf("unknown template: %s", templateName)
	}

	now := time.Now()
	data := EmailData{
		FirstName:      profile.FirstName,
		LastName:       profile.LastName,
		FullName:       profile.FullName(),
		Email:          profile.Email,
		OtherEmails:    strings.Join(profile.AdditionalEmails, ", "),
		OtherNames:     strings.Join(profile.NameVariants, ", "),
		OtherAddresses: strings.Join(profile.PreviousAddresses, "; "),
		Address:        profile.Address,
		City:           profile.City,
		State:          profile.State,
		ZipCode:        profile.ZipCode,
		Country:        profile.Country,
		Phone:          profile.Phone,
		OtherPhones:    strings.Join(profile.AdditionalPhones, ", "),
		DateOfBirth:    profile.DateOfBirth,
		BrokerName:     b.Name,
		BrokerEmail:    b.Email,
		BrokerWebsite:  b.Website,
		BrokerOptOut:   b.OptOutURL,
		Date:           now.Format("January 2, 2006"),
		Year:           now.Year(),
		Month:          now.Format("January"),
		Template:       templateName,
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("failed to render template: %w", err)
	}

	subject := e.getSubject(templateName, b.Name)

	return &Email{
		Subject: subject,
		Body:    buf.String(),
	}, nil
}

func (e *Engine) getSubject(templateName, brokerName string) string {
	switch templateName {
	case "gdpr":
		return "GDPR Data Erasure Request - Article 17 Right to Erasure"
	case "ccpa":
		return "CCPA Data Deletion Request - Right to Delete Personal Information"
	case "uk-access":
		return "Subject Access Request - Article 15 UK GDPR"
	case "uk-erasure":
		return "Request for Erasure - Article 17 UK GDPR"
	case "uk-combined":
		return "Subject Access Request (Art. 15) and Request for Erasure (Art. 17) - UK GDPR"
	default:
		return "Personal Data Removal Request"
	}
}

// RequestTypeFor reports which right a template exercises. Unknown templates
// are treated as erasure: every template that shipped before request types
// existed was a deletion request, and existing history rows default to the
// same value, so an unrecognised custom template keeps the old behaviour
// rather than silently creating a second sendable category per broker.
func RequestTypeFor(templateName string) string {
	switch templateName {
	case "uk-access":
		return history.RequestAccess
	case "uk-combined":
		return history.RequestCombined
	default:
		return history.RequestErasure
	}
}

// TemplateNames lists every built-in template, for CLI help and validation.
func TemplateNames() []string {
	return []string{"gdpr", "ccpa", "generic", "uk-access", "uk-erasure", "uk-combined"}
}

// IsKnownTemplate reports whether name is a built-in template.
func (e *Engine) IsKnownTemplate(name string) bool {
	_, ok := e.templates[name]
	return ok
}
