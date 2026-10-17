package browser

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
	"github.com/drumandbytes/eraser/internal/config"
)

// URLPolicy is scoped to one broker, never the whole directory. Only the hosts in
// Domains (and their www. form) are allowed, not subdomains: on shared platforms a
// subdomain is another company's tenant. LocalOrigin is only for the demo/test server.
type URLPolicy struct {
	Domains []string
	// ExactPaths pins page loads on a shared-platform host to the paths of the
	// catalog URLs: the host serves other companies' forms under other paths.
	ExactPaths  map[string][]string
	LocalOrigin string
}

func NewURLPolicy(website, portal string) URLPolicy {
	p := URLPolicy{Domains: BrokerDomains(website, portal), ExactPaths: map[string][]string{}}
	for _, raw := range []string{website, portal} {
		if u, err := url.Parse(raw); err == nil && isSharedPlatform(u.Hostname()) {
			host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
			p.ExactPaths[host] = append(p.ExactPaths[host], pathPrefix(u.Path))
		}
	}
	return p
}

// pathPrefix keeps the first two path segments: /webform/<id>/<step> -> /webform/<id>.
func pathPrefix(path string) string {
	segs := strings.Split(strings.Trim(path, "/"), "/")
	if segs[0] == "" {
		return "/"
	}
	return "/" + strings.Join(segs[:min(2, len(segs))], "/")
}

func (p URLPolicy) pathAllowed(host, path string) bool {
	prefixes, pinned := p.ExactPaths[host]
	if !pinned {
		return true
	}
	for _, pre := range prefixes {
		if pre == "/" || path == pre || strings.HasPrefix(path, pre+"/") {
			return true
		}
	}
	return false
}

// ValidateNavigation is Validate plus the path pin, for page loads and frames.
// Subresources and XHR only need the host, since platforms serve shared assets.
func (p URLPolicy) ValidateNavigation(ctx context.Context, raw string) error {
	if err := p.Validate(ctx, raw); err != nil {
		return err
	}
	if u, err := url.Parse(raw); err == nil && !p.pathAllowed(strings.ToLower(u.Hostname()), u.Path) {
		return fmt.Errorf("page is outside this broker's form on a shared platform")
	}
	return nil
}

// multi-tenant privacy/form services: page loads on these are pinned to the catalog URL's path
var sharedPlatforms = []string{"onetrust.com", "trustarc.com", "datasubject.com", "privacypillar.com", "saymine.io", "stirista.com", "optoutprescreen.com", "forms.gle", "google.com", "hubspot.com", "zendesk.com"}

func isSharedPlatform(host string) bool {
	for _, p := range sharedPlatforms {
		if host == p || strings.HasSuffix(host, "."+p) {
			return true
		}
	}
	return false
}

// connectSources is the CSP connect-src list: same hosts as allowsHost.
func (p URLPolicy) connectSources() []string {
	sources := []string{"'self'"}
	for _, d := range p.Domains {
		sources = append(sources, "https://"+d, "https://www."+d)
	}
	if p.LocalOrigin != "" {
		sources = append(sources, p.LocalOrigin)
	}
	return sources
}

func (p URLPolicy) allowsHost(host string) bool {
	host = strings.ToLower(host)
	for _, d := range p.Domains {
		if host == d || host == "www."+d {
			return true
		}
	}
	return false
}

func (p URLPolicy) Validate(ctx context.Context, raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return fmt.Errorf("only absolute HTTP(S) URLs without credentials are allowed")
	}
	if p.LocalOrigin != "" {
		if u.Scheme+"://"+u.Host == p.LocalOrigin {
			return nil
		}
		return fmt.Errorf("the demo can only access its own local form server")
	}
	if u.Scheme != "https" {
		return fmt.Errorf("personal data requires HTTPS")
	}
	if !p.allowsHost(u.Hostname()) {
		return fmt.Errorf("destination is outside this broker's approved domains")
	}
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, u.Hostname())
	if err != nil || len(ips) == 0 {
		return fmt.Errorf("cannot verify destination address")
	}
	for _, ip := range ips {
		if !ip.IP.IsGlobalUnicast() || ip.IP.IsPrivate() || ip.IP.IsLoopback() || ip.IP.IsLinkLocalUnicast() {
			return fmt.Errorf("private and local destinations are not allowed")
		}
	}
	return nil
}

type Control struct {
	Ref      string   `json:"ref"`
	Tag      string   `json:"tag"`
	Type     string   `json:"type"`
	Label    string   `json:"label"`
	Value    string   `json:"value"`
	Required bool     `json:"required"`
	Options  []string `json:"options,omitempty"`
}

type Snapshot struct {
	ID                string    `json:"snapshot_id"`
	URL               string    `json:"url"`
	Title             string    `json:"title"`
	Text              string    `json:"text"`
	Controls          []Control `json:"controls"`
	Blocker           string    `json:"needs_user,omitempty"`
	Screenshot        string    `json:"screenshot,omitempty"`
	Validation        []string  `json:"validation_errors,omitempty"`
	SubmissionBlocked bool      `json:"submission_blocked,omitempty"`
	SensitiveControls bool      `json:"sensitive_controls,omitempty"`
}

// Fingerprint ignores IDs and screenshots so re-observing a page can't dodge the duplicate-action check.
func (s Snapshot) Fingerprint() string {
	s.ID, s.Screenshot = "", ""
	b, _ := json.Marshal(s)
	return fmt.Sprintf("%x", sha256.Sum256(b))
}

// AutomationSession keeps one Chrome tab across MCP calls. Actions use refs from
// a fresh snapshot; agents never supply JavaScript.
type AutomationSession struct {
	b        *Browser
	policy   URLPolicy
	world    runtime.ExecutionContextID
	snapshot string
	mu       sync.Mutex
	blocked  string
	// set when Chrome hit a non-public address; unlike a destination pause it refuses actions
	networkBlocked string
}

func NewAutomationSession(cfg BrowserConfig, p *config.Profile, policy URLPolicy) (*AutomationSession, error) {
	if len(policy.Domains) == 0 && policy.LocalOrigin == "" {
		return nil, fmt.Errorf("broker has no approved domains")
	}
	b, err := New(cfg, p, policy.Domains)
	if err != nil {
		return nil, err
	}
	return &AutomationSession{b: b, policy: policy}, nil
}

func (s *AutomationSession) Close() { s.b.Close() }

// operation retains the browser's lifetime context but obeys tool cancellation.
func (s *AutomationSession) operation(caller context.Context) (context.Context, func()) {
	ctx, cancel := context.WithTimeout(s.b.ctx, s.b.config.Timeout)
	stop := context.AfterFunc(caller, cancel)
	return ctx, func() { stop(); cancel() }
}

func (s *AutomationSession) Open(caller context.Context, raw string) (*Snapshot, error) {
	if err := s.policy.ValidateNavigation(caller, raw); err != nil {
		return nil, err
	}
	// The first Run must own the lifetime context, not a single tool call.
	if err := chromedp.Run(s.b.ctx); err != nil {
		return nil, fmt.Errorf("start Chrome: %w", err)
	}
	chromedp.ListenTarget(s.b.ctx, func(event any) {
		switch e := event.(type) {
		case *fetch.EventRequestPaused:
			go s.checkRequest(e)
		case *network.EventResponseReceived:
			s.noteResponse(e)
		}
	})
	ctx, cancel := s.operation(caller)
	defer cancel()
	patterns := []*fetch.RequestPattern{{URLPattern: "*", RequestStage: fetch.RequestStageRequest}, {URLPattern: "*", ResourceType: network.ResourceTypeDocument, RequestStage: fetch.RequestStageResponse}}
	if err := chromedp.Run(ctx, network.Enable(), network.SetBypassServiceWorker(true), fetch.Enable().WithPatterns(patterns), chromedp.Navigate(raw), chromedp.WaitReady("body")); err != nil {
		return nil, fmt.Errorf("open task page: %w", err)
	}
	return s.Observe(caller)
}

// csp is added to every document the session loads. It keeps popups, workers
// and out-of-scope connections from escaping the tab's request interceptor;
// policies stack, so the site's own stays in force.
func (p URLPolicy) csp() string {
	return "sandbox allow-scripts allow-same-origin allow-forms allow-modals; worker-src 'none'; connect-src " + strings.Join(p.connectSources(), " ")
}

// restrictDocument adds the CSP to a document response. Chrome only enforces the
// sandbox part of a header changed through ContinueResponse and ignores
// worker-src and connect-src from it, so the response is fulfilled with its own
// body and the new header instead, which makes the whole policy count.
func (s *AutomationSession) restrictDocument(ctx context.Context, e *fetch.EventRequestPaused) {
	headers := append(e.ResponseHeaders, &fetch.HeaderEntry{Name: "Content-Security-Policy", Value: s.policy.csp()})
	var err error
	if e.ResponseStatusCode >= 300 && e.ResponseStatusCode < 400 {
		// A redirect has no page; the next hop comes back through the interceptor.
		if err := fetch.ContinueResponse(e.RequestID).Do(ctx); err != nil {
			_ = fetch.FailRequest(e.RequestID, network.ErrorReasonBlockedByClient).Do(ctx)
		}
		return
	}
	body, bodyErr := fetch.GetResponseBody(e.RequestID).Do(ctx)
	switch {
	case bodyErr != nil:
		// Nothing to read (an empty response): the sandbox still applies.
		err = fetch.ContinueResponse(e.RequestID).WithResponseCode(e.ResponseStatusCode).WithResponseHeaders(headers).Do(ctx)
	default:
		// GetResponseBody returns the decoded body, so drop the encoding headers.
		kept := make([]*fetch.HeaderEntry, 0, len(headers))
		for _, h := range headers {
			if n := strings.ToLower(h.Name); n != "content-encoding" && n != "content-length" {
				kept = append(kept, h)
			}
		}
		err = fetch.FulfillRequest(e.RequestID, e.ResponseStatusCode).WithResponseHeaders(kept).WithBody(base64.StdEncoding.EncodeToString(body)).Do(ctx)
	}
	if err != nil {
		_ = fetch.FailRequest(e.RequestID, network.ErrorReasonBlockedByClient).Do(ctx)
	}
}

// noteResponse stops the session when Chrome connected to a non-public address.
// Our DNS lookup and Chrome's can differ, so check the address Chrome used.
func (s *AutomationSession) noteResponse(e *network.EventResponseReceived) {
	if e.Response != nil && s.policy.LocalOrigin == "" && !publicAddress(e.Response.RemoteIPAddress) {
		s.mu.Lock()
		s.networkBlocked = "The page connected to a private network address, so it was blocked. Review the broker's URLs."
		s.mu.Unlock()
	}
}

func (s *AutomationSession) checkRequest(e *fetch.EventRequestPaused) {
	ctx, cancel := context.WithTimeout(s.b.ctx, 10*time.Second)
	defer cancel()
	execCtx := cdp.WithExecutor(ctx, chromedp.FromContext(ctx).Target)
	if e.ResponseStatusCode != 0 {
		s.restrictDocument(execCtx, e)
		return
	}
	// every request: redirects, frames, XHR, form actions. third-party portals must be in the catalog
	validate := s.policy.Validate
	if e.ResourceType == network.ResourceTypeDocument {
		validate = s.policy.ValidateNavigation
	}
	if err := validate(ctx, e.Request.URL); err != nil {
		s.mu.Lock()
		s.blocked = "A destination outside the approved broker/portal was blocked. Review the broker's configured URLs."
		s.mu.Unlock()
		_ = fetch.FailRequest(e.RequestID, network.ErrorReasonBlockedByClient).Do(execCtx)
		return
	}
	// Chrome only pauses a document again at the response stage, where the CSP
	// is added, when the request asks for it.
	cont := fetch.ContinueRequest(e.RequestID)
	if e.ResourceType == network.ResourceTypeDocument {
		cont = cont.WithInterceptResponse(true)
	}
	_ = cont.Do(execCtx)
}

func (s *AutomationSession) evaluate(ctx context.Context, expression string, out any) error {
	return chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		r, exception, err := runtime.Evaluate(expression).WithContextID(s.world).WithReturnByValue(true).Do(ctx)
		if err != nil {
			return err
		}
		if exception != nil {
			return fmt.Errorf("page changed; observe again before continuing")
		}
		if out == nil {
			return nil
		}
		return json.Unmarshal(r.Value, out)
	}))
}

func (s *AutomationSession) Observe(caller context.Context) (*Snapshot, error) {
	// Never hand the agent text or a screenshot from a private-address page.
	if blocked := s.blockedReason(); blocked != "" {
		return nil, fmt.Errorf("%s", blocked)
	}
	ctx, cancel := s.operation(caller)
	defer cancel()
	var current string
	if err := chromedp.Run(ctx, chromedp.Location(&current)); err != nil {
		return nil, err
	}
	if err := s.policy.ValidateNavigation(ctx, current); err != nil {
		return nil, err
	}
	if err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		tree, err := page.GetFrameTree().Do(ctx)
		if err != nil {
			return err
		}
		s.world, err = page.CreateIsolatedWorld(tree.Frame.ID).WithWorldName("eraser-automation").Do(ctx)
		return err
	})); err != nil {
		return nil, err
	}
	var snap Snapshot
	if err := s.evaluate(ctx, observeAutomationJS, &snap); err != nil {
		return nil, err
	}
	s.snapshot = rand.Text()
	snap.ID = s.snapshot
	var captcha string
	if err := s.evaluate(ctx, detectCaptchaJS, &captcha); err != nil {
		return nil, err
	}
	if (CaptchaInfo{Found: captcha != "", Type: captcha}).IsCaptchaBlocking() {
		var solved bool
		if err := s.evaluate(ctx, `Array.from(document.querySelectorAll('[name="g-recaptcha-response"], [name="h-captcha-response"], [name="cf-turnstile-response"]')).some(e => !!e.value)`, &solved); err != nil {
			return nil, err
		}
		if !solved {
			snap.Blocker = "Solve the CAPTCHA in the open Chrome window, then observe again."
		}
	}
	s.mu.Lock()
	if s.blocked != "" {
		snap.Blocker = s.blocked
	}
	s.mu.Unlock()
	if s.b.config.ScreenshotDir != "" && !snap.SensitiveControls {
		name, err := s.b.takeScreenshotQuality(ctx, "mcp", rand.Text(), 100)
		if err != nil {
			return nil, fmt.Errorf("save task evidence: %w", err)
		}
		snap.Screenshot = name
	}
	return &snap, nil
}

func (s *AutomationSession) Act(caller context.Context, snapshot, ref, action, value string) (*Snapshot, error) {
	if blocked := s.blockedReason(); blocked != "" {
		return nil, fmt.Errorf("%s", blocked)
	}
	if snapshot == "" || snapshot != s.snapshot {
		return nil, fmt.Errorf("stale snapshot; observe the page again")
	}
	ctx, cancel := s.operation(caller)
	defer cancel()
	arg, _ := json.Marshal(map[string]string{"ref": ref, "action": action, "value": value})
	var target struct {
		URL   string `json:"url"`
		Error string `json:"error"`
	}
	if err := s.evaluate(ctx, `((a) => { const e = globalThis.__eraserNodes?.[Number(a.ref.slice(1))]; if (!e || !e.isConnected || !/^e\d+$/.test(a.ref)) return {error:'Control changed; observe again'}; return {url: e.href || e.formAction || e.form?.action || location.href}; })(`+string(arg)+`)`, &target); err != nil {
		return nil, err
	}
	if target.Error != "" {
		return nil, fmt.Errorf("%s", target.Error)
	}
	if err := s.policy.Validate(ctx, target.URL); err != nil {
		return nil, err
	}
	var result string
	if err := s.evaluate(ctx, `(`+actAutomationJS+`)(`+string(arg)+`)`, &result); err != nil {
		return nil, err
	}
	if result != "" && result != "validation" {
		return nil, fmt.Errorf("%s", result)
	}
	s.snapshot = "" // Any action invalidates all previous references.
	if action == "click" {
		// let async navigation/validation settle (bounded)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(750 * time.Millisecond):
		}
	}
	snap, err := s.Observe(caller)
	if snap != nil {
		snap.SubmissionBlocked = result == "validation"
	}
	return snap, err
}

const observeAutomationJS = `(() => {
 const visible = e => !!(e.getClientRects().length) && getComputedStyle(e).visibility !== 'hidden';
 const nodes = Array.from(document.querySelectorAll('input,select,textarea,button,a[href],[role="button"]')).filter(visible).slice(0,150);
 globalThis.__eraserNodes = nodes;
 const label = e => { if(e.labels?.[0]) { const l=e.labels[0].cloneNode(true); l.querySelectorAll('input,select,textarea,button').forEach(n=>n.remove()); return l.textContent; } return e.getAttribute('aria-label')||e.innerText||e.placeholder||e.name||''; };
 const sensitive = e => ['password','file'].includes(e.type)||['one-time-code','current-password','new-password'].includes(e.autocomplete)||/^(otp|passcode|ssn|passport|national.?id|id.?number)$/i.test(e.name||e.id||'');
 const controls = nodes.map((e,i) => ({ref:'e'+i, tag:e.tagName.toLowerCase(), type:e.type||'', label:label(e).trim().replace(/\s+/g,' ').slice(0,200), value:sensitive(e)?'':String(e.value||'').slice(0,500), required:!!e.required, options:e.options?Array.from(e.options).map(o=>o.text):undefined}));
 let blocker = '';
 if (nodes.some(sensitive)) blocker = 'Login or identity verification requires your review in Chrome.';
 if (nodes.some(e=>e.autocomplete==='username'||/^(sign in|log in|login|authenticate)$/i.test(label(e).trim()))) blocker ||= 'Login required. Complete it in Chrome, then observe again.';
 if (nodes.some(e=>e.type==='password')) blocker = 'Login required. Complete it in Chrome, then observe again.';
 if (nodes.some(e=>e.type==='file')) blocker = 'Document/identity verification requires your review.';
 const solved = Array.from(document.querySelectorAll('[name="g-recaptcha-response"], [name="h-captcha-response"], [name="cf-turnstile-response"]')).some(e => !!e.value);
 if (Array.from(document.querySelectorAll('iframe')).some(e=>visible(e)&&!(solved&&/recaptcha|hcaptcha|challenges.cloudflare.com/.test(e.src)))) blocker ||= 'Embedded form or verification requires your review in Chrome.';
 return {url:location.href,title:document.title,text:(document.body?.innerText||'').slice(0,16000),controls,needs_user:blocker,sensitive_controls:nodes.some(sensitive),validation_errors:nodes.filter(e=>e.validity&&!e.validity.valid).map(e=>e.validationMessage).filter(Boolean)};
})()`

const actAutomationJS = `(a) => {
 const e = globalThis.__eraserNodes?.[Number(a.ref.slice(1))];
 if (!e || !e.isConnected || e.disabled || !e.getClientRects().length) return 'Control unavailable; observe again';
 if (['password','file'].includes(e.type)||['one-time-code','current-password','new-password'].includes(e.autocomplete)||/^(otp|passcode|ssn|passport|national.?id|id.?number)$/i.test(e.name||e.id||'')) return 'This field requires the user';
 if (a.action==='click') {
   if(e.type==='submit'&&e.form&&!e.form.noValidate&&!e.formNoValidate&&!e.form.checkValidity()) { e.form.reportValidity(); return 'validation'; }
   if(e.tagName==='A') { location.assign(e.href); } else { if(e.form) { e.form.target='_self'; e.formTarget='_self'; } e.click(); } return '';
 }
 if(a.action!=='fill') return 'Unsupported action';
 if(e.tagName==='SELECT') { const o=Array.from(e.options).find(o=>o.value===a.value||o.text.trim()===a.value); if(!o) return 'Option not found'; e.value=o.value; }
 else if(['checkbox','radio'].includes(e.type)) { if(!['true','false'].includes(a.value)) return 'Use true or false'; e.checked=a.value==='true'; }
 else if(['INPUT','TEXTAREA'].includes(e.tagName)) { const proto=e.tagName==='INPUT'?HTMLInputElement.prototype:HTMLTextAreaElement.prototype; Object.getOwnPropertyDescriptor(proto,'value').set.call(e,a.value); }
 else return 'Control is not a form field';
 e.dispatchEvent(new Event('input',{bubbles:true})); e.dispatchEvent(new Event('change',{bubbles:true})); return '';
}`

// BrokerDomains derives destinations only from trusted local catalog entries.
func BrokerDomains(website, portal string) []string {
	out := []string{}
	for _, raw := range []string{website, portal} {
		if u, err := url.Parse(raw); err == nil && u.Hostname() != "" {
			out = append(out, strings.TrimPrefix(strings.ToLower(u.Hostname()), "www."))
		}
	}
	return out
}

func (s *AutomationSession) blockedReason() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.networkBlocked
}

// publicAddress treats an empty address (cached or local response) as fine.
func publicAddress(raw string) bool {
	if raw == "" {
		return true
	}
	ip := net.ParseIP(raw)
	return ip != nil && ip.IsGlobalUnicast() && !ip.IsPrivate()
}
