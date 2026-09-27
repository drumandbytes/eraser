package browser

import (
	"fmt"
	"net/url"
	"strings"
)

// matchesAllowedDomain reports whether rawURL's host is an allowed domain or a
// subdomain of one. Shared by ValidateDomain and NavigateAndFill.
func matchesAllowedDomain(rawURL string, allowedDomains []string) (bool, string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false, "", fmt.Errorf("invalid URL: %w", err)
	}

	host := strings.ToLower(parsed.Host)

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

	for domain := range domainSet {
		if strings.HasSuffix(host, "."+domain) {
			return true, domain, nil
		}
	}

	return false, host, nil
}
