package browser

import (
	"fmt"
	"net/url"
	"strings"
)

// matchesAllowedDomain reports whether rawURL's host matches one of
// allowedDomains, either exactly or as a subdomain of one of them (e.g.
// "www.example.com" or "mail.example.com" match an allowed "example.com").
// It is the shared matching logic behind ConfirmationHandler.ValidateDomain
// and Browser.NavigateAndFill's pre-navigation allowlist check, so both
// paths apply identical rules to whatever URL they were handed.
func matchesAllowedDomain(rawURL string, allowedDomains []string) (bool, string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false, "", fmt.Errorf("invalid URL: %w", err)
	}

	host := strings.ToLower(parsed.Host)

	// Remove port if present
	if idx := strings.Index(host, ":"); idx != -1 {
		host = host[:idx]
	}

	domainSet := make(map[string]bool, len(allowedDomains))
	for _, d := range allowedDomains {
		domainSet[strings.ToLower(d)] = true
	}

	// Check exact match
	if domainSet[host] {
		return true, host, nil
	}

	// Check if it's a subdomain of a known domain
	for domain := range domainSet {
		if strings.HasSuffix(host, "."+domain) {
			return true, domain, nil
		}
	}

	return false, host, nil
}
