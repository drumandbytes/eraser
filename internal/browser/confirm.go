package browser

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"syscall"
	"time"
)

// isBlockedIP reports whether ip is anything other than a routable public
// address. A broker's confirmation link always lives on the public internet,
// so every one of these ranges is illegitimate as a target and is refused
// regardless of any user flag.
//
// net.IP's own predicates cover the common cases, including IPv6 unique-local
// (fc00::/7, via IsPrivate) and the IPv4-mapped IPv6 spellings of all of them
// (the predicates call To4 internally), so ::ffff:127.0.0.1 is caught as
// loopback without special handling.
func isBlockedIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() || ip.IsMulticast() {
		return true
	}

	// Ranges net.IP has no predicate for.
	if v4 := ip.To4(); v4 != nil {
		switch {
		case v4[0] == 0: // 0.0.0.0/8 "this network"
			return true
		case v4[0] == 100 && v4[1] >= 64 && v4[1] <= 127: // 100.64.0.0/10 CGNAT
			return true
		case v4[0] >= 240: // 240.0.0.0/4 reserved
			return true
		}
	}
	return false
}

// guardedTransport returns an http.Transport that refuses to open a
// connection to any non-public address.
//
// The check lives in Dialer.Control, which the runtime invokes after DNS
// resolution and before connect, once per candidate address. That placement
// matters for three reasons:
//
//   - It sees the IP actually being dialed, not the hostname, so an
//     attacker-controlled name with an A record pointing at 127.0.0.1 or
//     169.254.169.254 is caught where a string check on the URL is not.
//   - There is no TOCTOU gap: Go connects to the exact address handed to
//     Control, not to a name it re-resolves afterwards.
//   - It applies to every connection the client opens, including each
//     redirect hop, without the redirect policy having to re-check anything.
//
// Proxy is disabled deliberately. With HTTP_PROXY/HTTPS_PROXY set, the only
// address dialed is the proxy's and the real target travels inside the
// request, which would silently void the guarantee above.
func guardedTransport() *http.Transport {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.Proxy = nil
	tr.DialContext = (&net.Dialer{
		Timeout:   10 * time.Second,
		KeepAlive: 30 * time.Second,
		Control: func(_, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return fmt.Errorf("blocked connection to unparseable address %q", address)
			}
			ip := net.ParseIP(host)
			if ip == nil {
				return fmt.Errorf("blocked connection to unresolvable address %q", host)
			}
			if isBlockedIP(ip) {
				return fmt.Errorf("blocked connection to non-public address %s "+
					"(loopback/private/link-local targets are never allowed)", ip)
			}
			return nil
		},
	}).DialContext
	return tr
}

// ConfirmationResult holds the outcome of clicking a confirmation link
type ConfirmationResult struct {
	Success      bool
	URL          string
	FinalURL     string
	StatusCode   int
	ResponseBody string
	ErrorMessage string
	RedirectPath []string
}

// ConfirmationHandler handles clicking confirmation links from emails
type ConfirmationHandler struct {
	client        *http.Client
	brokerDomains map[string]bool
}

// NewConfirmationHandler creates a new handler with known broker domains
func NewConfirmationHandler(brokerDomains []string) *ConfirmationHandler {
	domains := make(map[string]bool)
	for _, d := range brokerDomains {
		// Store both the domain and common variations
		d = strings.ToLower(d)
		domains[d] = true
		// Also allow subdomains
		if !strings.HasPrefix(d, "www.") {
			domains["www."+d] = true
		}
		if !strings.HasPrefix(d, "mail.") {
			domains["mail."+d] = true
		}
		if !strings.HasPrefix(d, "email.") {
			domains["email."+d] = true
		}
	}

	return &ConfirmationHandler{
		client: &http.Client{
			Timeout:   30 * time.Second,
			Transport: guardedTransport(),
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				// Allow up to 10 redirects. Per-call policy (including the
				// domain check) is installed in ClickConfirmationLink.
				if len(via) >= 10 {
					return fmt.Errorf("too many redirects")
				}
				return nil
			},
		},
		brokerDomains: domains,
	}
}

// ValidateDomain checks if the URL belongs to a known broker domain
func (h *ConfirmationHandler) ValidateDomain(confirmURL string) (bool, string, error) {
	return matchesAllowedDomain(confirmURL, h.domainList())
}

// domainList returns the handler's known broker domains (including the
// www./mail./email. variants added in NewConfirmationHandler) as a slice,
// for use with the shared matchesAllowedDomain helper.
func (h *ConfirmationHandler) domainList() []string {
	domains := make([]string, 0, len(h.brokerDomains))
	for d := range h.brokerDomains {
		domains = append(domains, d)
	}
	return domains
}

// ClickConfirmationLink sends a GET request to the confirmation URL
func (h *ConfirmationHandler) ClickConfirmationLink(confirmURL string, validateDomain bool) (*ConfirmationResult, error) {
	result := &ConfirmationResult{
		URL:          confirmURL,
		RedirectPath: []string{confirmURL},
	}

	// Validate domain if requested
	if validateDomain {
		valid, domain, err := h.ValidateDomain(confirmURL)
		if err != nil {
			result.ErrorMessage = err.Error()
			return result, err
		}
		if !valid {
			result.ErrorMessage = fmt.Sprintf("domain %s is not a known broker domain", domain)
			return result, fmt.Errorf("domain %s is not a known broker domain", domain)
		}
	}

	// Create request with browser-like headers
	req, err := http.NewRequest("GET", confirmURL, nil)
	if err != nil {
		result.ErrorMessage = fmt.Sprintf("failed to create request: %v", err)
		return result, err
	}

	// Set headers to look like a real browser
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.5")
	req.Header.Set("Connection", "keep-alive")

	// Track redirects, applying the same domain rule to each hop that was
	// applied to the initial URL -- an open redirect on a broker site (or a
	// compromised first hop) could otherwise carry an identifying token to
	// an arbitrary third party across up to 10 hops.
	//
	// The hop check follows validateDomain rather than running
	// unconditionally, because most real broker confirmations arrive as a
	// click-tracker link (SendGrid, Mailgun) that redirects to a third-party
	// privacy portal (TrustArc, OneTrust) -- neither domain is in
	// brokers.yaml. Validating hops unconditionally made
	// --validate-domain=false useless for exactly the links it exists to
	// handle: the first hop was permitted and the redirect was then refused.
	//
	// Disabling the domain check never permits an internal target: the
	// transport's dial guard rejects loopback/private/link-local addresses
	// on every hop regardless of this flag, so the worst case here is a
	// token reaching an unexpected *public* host, which is the tradeoff the
	// user opts into by passing a non-default flag. Every hop is recorded in
	// RedirectPath so the chain stays auditable.
	//
	// Assigned on a copy of the client so concurrent calls can't clobber
	// each other's policy; the Transport pointer is shared, so connection
	// pooling and the dial guard are preserved.
	var redirects []string
	client := *h.client
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return fmt.Errorf("too many redirects")
		}

		if validateDomain {
			valid, domain, err := matchesAllowedDomain(req.URL.String(), h.domainList())
			if err != nil {
				return fmt.Errorf("redirect validation failed: %w", err)
			}
			if !valid {
				return fmt.Errorf("redirect to disallowed domain %s blocked "+
					"(pass --validate-domain=false if this broker confirms via a "+
					"third-party privacy portal)", domain)
			}
		}

		redirects = append(redirects, req.URL.String())
		return nil
	}

	// Make the request
	resp, err := client.Do(req)
	if err != nil {
		result.ErrorMessage = fmt.Sprintf("request failed: %v", err)
		return result, err
	}
	defer func() { _ = resp.Body.Close() }()

	result.StatusCode = resp.StatusCode
	result.FinalURL = resp.Request.URL.String()
	result.RedirectPath = append(result.RedirectPath, redirects...)

	// Read response body (limited to 64KB)
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		result.ErrorMessage = fmt.Sprintf("failed to read response: %v", err)
		return result, err
	}
	result.ResponseBody = string(body)

	// Check for success indicators
	result.Success = h.isSuccessResponse(resp.StatusCode, result.ResponseBody)

	if !result.Success && result.ErrorMessage == "" {
		result.ErrorMessage = fmt.Sprintf("confirmation may have failed (status %d)", resp.StatusCode)
	}

	return result, nil
}

// isSuccessResponse checks if the response indicates successful confirmation
func (h *ConfirmationHandler) isSuccessResponse(statusCode int, body string) bool {
	// Check HTTP status
	if statusCode < 200 || statusCode >= 400 {
		return false
	}

	bodyLower := strings.ToLower(body)

	// Success indicators
	successPatterns := []string{
		"successfully",
		"confirmed",
		"verification complete",
		"verified",
		"opt-out complete",
		"removal complete",
		"request received",
		"request confirmed",
		"thank you",
		"has been removed",
		"been deleted",
		"been processed",
		"unsubscribed",
		"opted out",
	}

	for _, pattern := range successPatterns {
		if strings.Contains(bodyLower, pattern) {
			return true
		}
	}

	// Failure indicators (if these are present, it's NOT a success)
	failurePatterns := []string{
		"link expired",
		"link invalid",
		"already confirmed",
		"error occurred",
		"something went wrong",
		"could not",
		"unable to",
		"failed",
	}

	for _, pattern := range failurePatterns {
		if strings.Contains(bodyLower, pattern) {
			return false
		}
	}

	// If status is 200 and no failure patterns, assume success
	return statusCode == 200
}

// ExtractConfirmationStatus extracts a human-readable status from the response
func (h *ConfirmationHandler) ExtractConfirmationStatus(result *ConfirmationResult) string {
	if result.Success {
		return "Confirmation successful"
	}

	bodyLower := strings.ToLower(result.ResponseBody)

	if strings.Contains(bodyLower, "expired") {
		return "Link expired"
	}
	if strings.Contains(bodyLower, "already") {
		return "Already confirmed/processed"
	}
	if strings.Contains(bodyLower, "invalid") {
		return "Invalid link"
	}
	if result.StatusCode == 404 {
		return "Link not found (404)"
	}
	if result.StatusCode >= 500 {
		return "Server error"
	}

	return "Unknown status"
}
