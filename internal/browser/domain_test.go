package browser

import "testing"

// Table-driven coverage for matchesAllowedDomain, the shared allowlist logic
// behind both Browser.NavigateAndFill's pre-navigation check and
// ConfirmationHandler's redirect-hop re-validation. A regression here (e.g.
// a suffix check that accidentally allows "evilbroker.com" to match an
// allowed "broker.com") would silently reopen the PII-exfiltration hole
// these checks were added to close.
func TestMatchesAllowedDomain(t *testing.T) {
	tests := []struct {
		name           string
		rawURL         string
		allowedDomains []string
		wantMatch      bool
		wantDomain     string
		wantErr        bool
	}{
		{
			name:           "exact match",
			rawURL:         "https://broker.com/optout",
			allowedDomains: []string{"broker.com"},
			wantMatch:      true,
			wantDomain:     "broker.com",
		},
		{
			name:           "subdomain match",
			rawURL:         "https://sub.broker.com/optout",
			allowedDomains: []string{"broker.com"},
			wantMatch:      true,
			wantDomain:     "broker.com",
		},
		{
			name:           "deep subdomain match",
			rawURL:         "https://mail.confirm.broker.com/optout",
			allowedDomains: []string{"broker.com"},
			wantMatch:      true,
			wantDomain:     "broker.com",
		},
		{
			name:           "non-matching domain",
			rawURL:         "https://evil.com/optout",
			allowedDomains: []string{"broker.com"},
			wantMatch:      false,
			wantDomain:     "evil.com",
		},
		{
			name:           "similar-looking domain is not a suffix match",
			rawURL:         "https://evilbroker.com/optout",
			allowedDomains: []string{"broker.com"},
			wantMatch:      false,
			wantDomain:     "evilbroker.com",
		},
		{
			name:           "suffix without dot separator does not match",
			rawURL:         "https://notbroker.com/optout",
			allowedDomains: []string{"broker.com"},
			wantMatch:      false,
			wantDomain:     "notbroker.com",
		},
		{
			name:           "case-insensitive host",
			rawURL:         "https://BROKER.COM/optout",
			allowedDomains: []string{"broker.com"},
			wantMatch:      true,
			wantDomain:     "broker.com",
		},
		{
			name:           "case-insensitive allowlist entry",
			rawURL:         "https://broker.com/optout",
			allowedDomains: []string{"BROKER.COM"},
			wantMatch:      true,
			wantDomain:     "broker.com",
		},
		{
			name:           "port is stripped before matching",
			rawURL:         "https://broker.com:8443/optout",
			allowedDomains: []string{"broker.com"},
			wantMatch:      true,
			wantDomain:     "broker.com",
		},
		{
			name:           "empty allowlist matches nothing",
			rawURL:         "https://broker.com/optout",
			allowedDomains: []string{},
			wantMatch:      false,
			wantDomain:     "broker.com",
		},
		{
			name:           "nil allowlist matches nothing",
			rawURL:         "https://broker.com/optout",
			allowedDomains: nil,
			wantMatch:      false,
			wantDomain:     "broker.com",
		},
		{
			name:           "malformed URL with control character errors",
			rawURL:         "http://broker.com/\x7f",
			allowedDomains: []string{"broker.com"},
			wantErr:        true,
		},
		{
			name:           "relative URL with no host does not match and does not error",
			rawURL:         "/just/a/path",
			allowedDomains: []string{"broker.com"},
			wantMatch:      false,
			wantDomain:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			match, domain, err := matchesAllowedDomain(tt.rawURL, tt.allowedDomains)

			if (err != nil) != tt.wantErr {
				t.Fatalf("matchesAllowedDomain(%q) error = %v, wantErr %v", tt.rawURL, err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if match != tt.wantMatch {
				t.Errorf("matchesAllowedDomain(%q, %v) match = %v, want %v", tt.rawURL, tt.allowedDomains, match, tt.wantMatch)
			}
			if domain != tt.wantDomain {
				t.Errorf("matchesAllowedDomain(%q, %v) domain = %q, want %q", tt.rawURL, tt.allowedDomains, domain, tt.wantDomain)
			}
		})
	}
}

// matchesAllowedDomain returns false for an empty/nil allowlist, and
// Browser.NavigateAndFill now agrees with it: an empty list rejects every
// navigation rather than being read as "no restriction configured". This test
// pins down the lower-level function's own behavior; the call site's matching
// fail-closed policy lives in browser.go.
func TestMatchesAllowedDomain_EmptyAllowlistIsDocumentedAsNoMatch(t *testing.T) {
	match, _, err := matchesAllowedDomain("https://anything.example.com", []string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if match {
		t.Error("matchesAllowedDomain with an empty allowlist returned true; expected false (callers that want to skip the check must special-case an empty list themselves)")
	}
}
