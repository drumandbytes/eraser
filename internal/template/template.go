package template

import (
	"bytes"
	"embed"
	"fmt"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/drumandbytes/eraser/internal/broker"
	"github.com/drumandbytes/eraser/internal/config"
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
	mu        sync.RWMutex
	templates map[string]*template.Template
	subject   string // subject of the custom template, "" = default
}

// CustomName is the template name backed by Options.CustomTemplate.
const CustomName = "custom"

// NewEngine creates a new template engine
func NewEngine() (*Engine, error) {
	e := &Engine{
		templates: make(map[string]*template.Template),
	}

	templateNames := []string{"gdpr", "ccpa", "generic"}
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

// ParseCustom splits an optional leading "Subject: ..." line off body and
// parses the rest as a template.
func ParseCustom(body string) (*template.Template, string, error) {
	body = strings.ReplaceAll(body, "\r\n", "\n")
	subject := ""
	if first, rest, _ := strings.Cut(body, "\n"); strings.HasPrefix(strings.ToLower(first), "subject:") {
		subject = strings.TrimSpace(first[len("subject:"):])
		body = strings.TrimLeft(rest, "\n")
	}
	if strings.TrimSpace(body) == "" {
		return nil, "", fmt.Errorf("template body is empty")
	}
	tmpl, err := template.New(CustomName).Option("missingkey=error").Parse(body)
	if err != nil {
		return nil, "", fmt.Errorf("invalid template: %w", err)
	}
	return tmpl, subject, nil
}

// SetCustom installs (or, with an empty body, removes) the custom template.
func (e *Engine) SetCustom(body string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if strings.TrimSpace(body) == "" {
		delete(e.templates, CustomName)
		e.subject = ""
		return nil
	}
	tmpl, subject, err := ParseCustom(body)
	if err != nil {
		return err
	}
	e.templates[CustomName], e.subject = tmpl, subject
	return nil
}

// UseConfig registers cfg's custom template, if it has one.
func (e *Engine) UseConfig(cfg *config.Config) error {
	if cfg == nil {
		return nil
	}
	return e.SetCustom(cfg.Options.CustomTemplate)
}

// Render generates an email from a template
func (e *Engine) Render(templateName string, profile config.Profile, b broker.Broker) (*Email, error) {
	e.mu.RLock()
	tmpl, ok := e.templates[templateName]
	customSubject := e.subject
	e.mu.RUnlock()
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
	if templateName == CustomName && customSubject != "" {
		subject = customSubject
	}

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
	default:
		return "Personal Data Removal Request"
	}
}
