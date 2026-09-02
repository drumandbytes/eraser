package web

import (
	"context"
	"crypto/rand"
	"embed"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	csrf "filippo.io/csrf/gorilla"
	"github.com/drumandbytes/eraser/internal/broker"
	"github.com/drumandbytes/eraser/internal/config"
	"github.com/drumandbytes/eraser/internal/history"
	emaTemplate "github.com/drumandbytes/eraser/internal/template"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

//go:embed static/*
var staticFS embed.FS

//go:embed templates/*
var templatesFS embed.FS

const (
	defaultRateLimit  = 30
	defaultRateWindow = time.Minute
	defaultSessionTTL = 30 * time.Minute
)

type RateLimiter struct {
	mu       sync.Mutex
	requests map[string][]time.Time
	limit    int
	window   time.Duration
}

func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	rl := &RateLimiter{
		requests: make(map[string][]time.Time),
		limit:    limit,
		window:   window,
	}
	go rl.cleanupLoop()
	return rl
}

func (rl *RateLimiter) filterRecent(times []time.Time, windowStart time.Time) []time.Time {
	n := 0
	for _, t := range times {
		if t.After(windowStart) {
			times[n] = t
			n++
		}
	}
	return times[:n]
}

func (rl *RateLimiter) Allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	recent := rl.filterRecent(rl.requests[key], now.Add(-rl.window))

	if len(recent) >= rl.limit {
		rl.requests[key] = recent
		return false
	}
	rl.requests[key] = append(recent, now)
	return true
}

func (rl *RateLimiter) cleanupLoop() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		rl.mu.Lock()
		windowStart := time.Now().Add(-rl.window)
		for key, times := range rl.requests {
			recent := rl.filterRecent(times, windowStart)
			if len(recent) == 0 {
				delete(rl.requests, key)
			} else {
				rl.requests[key] = recent
			}
		}
		rl.mu.Unlock()
	}
}

// Version is the build version shown in the web UI footer. main sets it from
// its own -ldflags-injected version at startup; it stays "dev" otherwise.
var Version = "dev"

type Server struct {
	config         atomic.Pointer[config.Config]
	configPath     string
	brokerDB       *broker.BrokerDatabase
	historyStore   *history.Store
	tmplEngine     *emaTemplate.Engine
	templates      map[string]*template.Template
	httpServer     *http.Server
	port           int
	csrfKey        []byte
	sessions       *SessionStore
	rateLimiter    *RateLimiter
	jobManager     *JobManager
	jobPersistence *JobPersistence
}

func NewServer(port int, cfg *config.Config, configPath string, brokerDB *broker.BrokerDatabase, historyStore *history.Store, tmplEngine *emaTemplate.Engine) (*Server, error) {
	csrfKey := make([]byte, 32)
	if _, err := rand.Read(csrfKey); err != nil {
		return nil, fmt.Errorf("failed to generate CSRF key: %w", err)
	}

	// Job persistence lives alongside the config file, so an alternate
	// --config also gets its own isolated pending_job.json instead of
	// always sharing ~/.eraser (matches history.DBPathFor's same reasoning
	// for history.db). configPath is only ever "" from tests constructing
	// a Server directly - preserve the old default there.
	dataDir := filepath.Dir(configPath)
	if configPath == "" {
		home, _ := os.UserHomeDir()
		dataDir = filepath.Join(home, ".eraser")
	}

	s := &Server{
		configPath:     configPath,
		brokerDB:       brokerDB,
		historyStore:   historyStore,
		tmplEngine:     tmplEngine,
		port:           port,
		csrfKey:        csrfKey,
		sessions:       NewSessionStore(defaultSessionTTL),
		rateLimiter:    NewRateLimiter(defaultRateLimit, defaultRateWindow),
		jobManager:     NewJobManager(),
		jobPersistence: NewJobPersistence(dataDir),
	}
	s.config.Store(cfg)

	tmpl, err := s.parseTemplates()
	if err != nil {
		return nil, fmt.Errorf("failed to parse templates: %w", err)
	}
	s.templates = tmpl
	return s, nil
}

// getConfig returns the server's current config. Server.config is an
// atomic.Pointer[config.Config] rather than a plain *config.Config because
// it's written concurrently (handleSettingsInbox, handleSetupComplete) while
// being read by many handlers and by background send-job goroutines for the
// duration of a send - see the load-copy-mutate-store pattern at the two
// write sites for how updates stay race-free.
func (s *Server) getConfig() *config.Config {
	return s.config.Load()
}

// maxFormBodyBytes caps the size of request bodies read by ParseForm, so a
// misbehaving or malicious client can't make a form-parsing handler buffer
// an unbounded body in memory.
const maxFormBodyBytes = 1 << 20 // 1MB

// limitFormBody wraps r.Body in a MaxBytesReader before the handler calls
// r.ParseForm()/r.FormValue(). w is passed through to MaxBytesReader so it
// can send a 413 response if the client keeps writing past the limit.
func limitFormBody(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBodyBytes)
}

// parseTemplates loads and parses all HTML templates
// Each page gets its own template set to avoid "content" block conflicts
func (s *Server) parseTemplates() (map[string]*template.Template, error) {
	funcs := template.FuncMap{
		"formatTime": func(t time.Time) string {
			return t.Format("Jan 2, 2006 3:04 PM")
		},
		"formatDate": func(t time.Time) string {
			return t.Format("Jan 2, 2006")
		},
		"add": func(a, b int) int {
			return a + b
		},
		// percent formats a 0..1 confidence score as a whole-number percentage.
		"percent": func(f float64) string {
			return fmt.Sprintf("%.0f%%", f*100)
		},
		// dict builds a map from alternating key/value args so a partial can be
		// invoked with more than one parameter, e.g.
		// {{template "partials/broker-row.html" (dict "Broker" . "IDPrefix" "mobile-")}}
		"dict": func(values ...interface{}) (map[string]interface{}, error) {
			if len(values)%2 != 0 {
				return nil, fmt.Errorf("dict: got %d args, want an even number of key/value pairs", len(values))
			}
			m := make(map[string]interface{}, len(values)/2)
			for i := 0; i < len(values); i += 2 {
				key, ok := values[i].(string)
				if !ok {
					return nil, fmt.Errorf("dict: key at position %d is %T, want string", i, values[i])
				}
				m[key] = values[i+1]
			}
			return m, nil
		},
	}

	// Read layout template
	layoutContent, err := templatesFS.ReadFile("templates/layout.html")
	if err != nil {
		return nil, fmt.Errorf("failed to read layout template: %w", err)
	}

	// Read all partial templates, keyed by their name relative to templates/
	// (e.g. "partials/broker-list.html").
	partialTemplates := make(map[string]string)
	err = fs.WalkDir(templatesFS, "templates/partials", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".html") {
			return err
		}
		content, err := templatesFS.ReadFile(path)
		if err != nil {
			return err
		}
		partialTemplates[path[len("templates/"):]] = string(content)
		return nil
	})
	if err != nil && !strings.Contains(err.Error(), "file does not exist") {
		return nil, fmt.Errorf("failed to read partials: %w", err)
	}

	// Map to hold all page templates
	templates := make(map[string]*template.Template)

	// Walk through all page templates and create separate template sets
	err = fs.WalkDir(templatesFS, "templates", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		// Skip directories, partials, and layout
		if d.IsDir() || strings.Contains(path, "/partials/") || path == "templates/layout.html" {
			return nil
		}
		if !strings.HasSuffix(path, ".html") {
			return nil
		}

		content, err := templatesFS.ReadFile(path)
		if err != nil {
			return fmt.Errorf("failed to read template %s: %w", path, err)
		}

		// Create a new template for this page
		name := path[len("templates/"):]
		pageTmpl := template.New(name).Funcs(funcs)

		// Parse layout first
		_, err = pageTmpl.Parse(string(layoutContent))
		if err != nil {
			return fmt.Errorf("failed to parse layout for %s: %w", name, err)
		}

		// Parse each partial as a named associated template, so a page can invoke
		// it as {{template "partials/x.html" .}} (or with (dict ...) for multiple
		// args). See docs/code-patterns.md.
		for pName, pContent := range partialTemplates {
			if _, err = pageTmpl.New(pName).Parse(pContent); err != nil {
				return fmt.Errorf("failed to parse partial %s for %s: %w", pName, name, err)
			}
		}

		// Parse the page content (this defines "content" block for this specific page)
		_, err = pageTmpl.Parse(string(content))
		if err != nil {
			return fmt.Errorf("failed to parse template %s: %w", name, err)
		}

		// Store in map
		templates[name] = pageTmpl

		return nil
	})

	if err != nil {
		return nil, err
	}

	// Add each partial as a standalone template for HTMX fragment responses.
	// Every partial is associated into the set so one partial can invoke another
	// with {{template "partials/x.html" .}} (e.g. broker-actions.html reuses the
	// desktop/mobile action clusters).
	for entry := range partialTemplates {
		set := template.New("").Funcs(funcs)
		for pName, pContent := range partialTemplates {
			if _, err = set.New(pName).Parse(pContent); err != nil {
				return nil, fmt.Errorf("failed to parse partial %s: %w", pName, err)
			}
		}
		templates[entry] = set.Lookup(entry)
	}

	return templates, nil
}

// Start starts the web server and opens the browser
func (s *Server) Start() error {
	router := s.setupRouter()

	s.httpServer = &http.Server{
		Addr:         fmt.Sprintf("127.0.0.1:%d", s.port),
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Check for pending job and offer to resume
	s.checkPendingJob()

	// Open browser after a short delay
	go func() {
		time.Sleep(500 * time.Millisecond)
		url := fmt.Sprintf("http://localhost:%d", s.port)
		openBrowser(url)
	}()

	fmt.Printf("Starting Eraser web UI at http://localhost:%d\n", s.port)
	fmt.Println("Press Ctrl+C to stop")

	if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("server error: %w", err)
	}

	return nil
}

// Shutdown gracefully shuts down the server
func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}

// setupRouter configures all routes
func (s *Server) setupRouter() *chi.Mux {
	r := chi.NewRouter()

	// Middleware
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Compress(5))
	r.Use(securityHeaders)

	// CSRF protection. filippo.io/csrf/gorilla enforces same-origin requests
	// via the browser's Sec-Fetch-Site / Origin headers rather than tokens or
	// cookies (see https://go.dev/issue/73626). It "just works" for a
	// loopback plaintext server - no TrustedOrigins / PlaintextHTTPRequest
	// needed - and, being a different module, isn't affected by
	// CVE-2025-47909 in the unmaintained github.com/gorilla/csrf. The
	// per-form {{.CSRFField}} still renders (a stub value, ignored) so no
	// template changes were needed.
	r.Use(csrf.Protect(s.csrfKey))

	// Static files
	staticSub, _ := fs.Sub(staticFS, "static")
	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.FS(staticSub))))

	// Routes
	r.Get("/", s.handleDashboard)
	r.Get("/brokers", s.handleBrokers)
	r.Get("/brokers/{brokerID}/email", s.handleBrokerEmail)
	r.Get("/history", s.handleHistory)
	r.Get("/settings", s.handleSettings)
	r.Post("/settings/inbox", s.handleSettingsInbox)
	r.Get("/settings/profiles/new", s.handleSettingsProfileNew)
	r.Post("/settings/profiles/new", s.handleSettingsProfileNew)
	r.Get("/settings/profiles/{profileID}/edit", s.handleSettingsProfileEdit)
	r.Post("/settings/profiles/{profileID}/edit", s.handleSettingsProfileEdit)
	r.Post("/settings/profiles/{profileID}/delete", s.handleSettingsProfileDelete)
	r.Get("/pipeline", s.handlePipeline)
	r.Get("/tasks", s.handleTasks)
	r.Get("/tasks/{taskID}", s.handleTaskDetail)
	r.Get("/tasks/{taskID}/helper", s.handleTaskHelper)
	r.Post("/tasks/{taskID}/complete", s.handleTaskComplete)
	r.Post("/tasks/{taskID}/skip", s.handleTaskSkip)
	r.Get("/forms", s.handleForms)
	r.Post("/forms/{brokerID}/complete", s.handleFormComplete)
	r.Post("/forms/{brokerID}/skip", s.handleFormSkip)

	// Setup wizard routes
	r.Route("/setup", func(r chi.Router) {
		r.Get("/", s.handleSetupWelcome)
		r.Get("/profile", s.handleSetupProfile)
		r.Post("/profile", s.handleSetupProfile)
		r.Get("/email", s.handleSetupEmail)
		r.Post("/email", s.handleSetupEmail)
		r.Get("/test", s.handleSetupTest)
		r.Post("/test/send", s.handleSetupTestSend)
		r.Get("/complete", s.handleSetupComplete)
	})

	// API routes (for HTMX)
	r.Route("/api", func(r chi.Router) {
		r.Get("/brokers", s.handleAPIBrokers)
		r.Get("/brokers/{brokerID}/status", s.handleAPIBrokerStatus)
		r.Post("/brokers/{brokerID}/exclude", s.handleAPIExcludeBroker)
		r.Post("/brokers/{brokerID}/include", s.handleAPIIncludeBroker)
		r.Post("/brokers/{brokerID}/mark-sent", s.handleAPIMarkSent)
		r.Delete("/history/failed", s.handleAPIDeleteFailed)
		r.Delete("/history", s.handleAPIDeleteAllHistory)
		r.Post("/send/{brokerID}", s.handleAPISendOne)
		r.Post("/send-all", s.handleAPISendAll)
		r.Get("/job/active", s.handleAPIJobActive)
		r.Get("/job/{jobID}/status", s.handleAPIJobStatus)
		r.Post("/job/{jobID}/cancel", s.handleAPIJobCancel)
		r.Get("/pipeline/responses", s.handleAPIResponses)
		r.Post("/inbox/scan", s.handleAPIInboxScan)
		r.Post("/inbox/rescan", s.handleAPIInboxRescan)
		r.Post("/inbox/reclassify", s.handleAPIReclassify)
		r.Post("/profile", s.handleAPISwitchProfile)
	})

	return r
}

// securityHeaders adds security headers to all responses
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Prevent clickjacking
		w.Header().Set("X-Frame-Options", "DENY")

		// Prevent MIME type sniffing
		w.Header().Set("X-Content-Type-Options", "nosniff")

		// Control referrer information
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")

		// Content Security Policy - restrict resource loading.
		// 'unsafe-inline' (style-src) covers layout.html's <style> block and
		// inline style attributes; 'unsafe-inline' (script-src) covers HTMX's
		// inline attributes and the small inline scripts in the templates.
		// No 'unsafe-eval'. All CSS and JS is self-hosted under /static/.
		csp := "default-src 'self'; " +
			"script-src 'self' 'unsafe-inline'; " +
			"style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; " +
			"img-src 'self' data:; " +
			"font-src 'self' https://fonts.gstatic.com; " +
			"connect-src 'self'; " +
			"object-src 'none'; " +
			"frame-ancestors 'none'; " +
			"form-action 'self'; " +
			"base-uri 'self'"
		w.Header().Set("Content-Security-Policy", csp)

		// Prevent caching of sensitive pages - credentials should never be cached
		// Static files are excluded from this via separate cache headers
		if !strings.HasPrefix(r.URL.Path, "/static/") {
			w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, private")
			w.Header().Set("Pragma", "no-cache")
			w.Header().Set("Expires", "0")
		}

		// Disable unnecessary browser features
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=()")

		next.ServeHTTP(w, r)
	})
}

// openBrowser opens the default browser to the specified URL
func openBrowser(url string) {
	var cmd string
	var args []string

	switch runtime.GOOS {
	case "darwin":
		cmd = "open"
		args = []string{url}
	case "linux":
		cmd = "xdg-open"
		args = []string{url}
	case "windows":
		cmd = "cmd"
		args = []string{"/c", "start", url}
	default:
		return
	}

	_ = exec.Command(cmd, args...).Start()
}

// activeProfileCookie names the cookie that remembers which profile the web
// UI is currently scoped to. Plain (not HttpOnly/Secure) since it carries no
// secret - just a UI preference - and the server always validates it against
// the configured profile list before trusting it, so a tampered value just
// falls back to the first profile rather than granting anything.
const activeProfileCookie = "eraser_profile"

// activeProfile resolves which profile the current request should act on.
// Unlike the CLI's --profile flag (where an ambiguous, unspecified profile
// is a hard error - see config.GetProfile), the web UI always has a
// definite answer: the eraser_profile cookie if it still names a configured
// profile, else the first configured profile. This is what every
// profile-scoped handler below should call instead of reaching for
// s.config.Profile directly.
func (s *Server) activeProfile(r *http.Request) config.NamedProfile {
	cfg := s.getConfig()
	if cfg == nil {
		return config.NamedProfile{ID: config.DefaultProfileID}
	}
	profiles := cfg.GetProfiles()
	if cookie, err := r.Cookie(activeProfileCookie); err == nil {
		for _, p := range profiles {
			if p.ID == cookie.Value {
				return p
			}
		}
	}
	return profiles[0]
}

// Helper methods

type Stats struct {
	TotalBrokers int
	Sent         int
	Failed       int
	Pending      int
}

// BrokerWithStatus combines broker info with history status
type BrokerWithStatus struct {
	broker.Broker
	Status     string // "never", "sent", "failed"
	LastSent   string // formatted date or empty
	TotalSent  int
	Excluded   bool // true if excluded via config.Options.ExcludedBrokers/ExcludedCategories
	ManualMode bool // config.Options.send_mode == "manual" - row shows "Email" + "Mark sent" instead of "Send"
}

// getBrokersWithStatus returns brokers with their history status. When
// showExcluded is false (the normal case - the default brokers view, and
// every send path), brokers matching ExcludedBrokers/ExcludedCategories are
// dropped entirely, same as broker.Filter. When true (the brokers page's
// "Show excluded" checkbox), they're included instead, with Excluded set,
// so the UI can render an Include button instead of Send.
func (s *Server) getBrokersWithStatus(profileID, search, category, region, statusFilter string, missingEmail, showExcluded bool) []BrokerWithStatus {
	// Get all broker statuses from history, scoped to the active profile
	var brokerStatuses map[string]history.BrokerStatus
	if s.historyStore != nil {
		brokerStatuses, _ = s.historyStore.GetAllBrokerStatuses(profileID)
	}
	if brokerStatuses == nil {
		brokerStatuses = make(map[string]history.BrokerStatus)
	}

	// excluded_brokers/excluded_categories (config.yaml) used to only be
	// enforced by the CLI's `send` command via broker.Filter - the web UI's
	// list and bulk-send both went through this function instead, which
	// never looked at either option, so a configured exclusion silently had
	// no effect here. Apply the same two checks broker.Filter does.
	var excludedIDs, excludedNames, excludedCats map[string]bool
	if cfg := s.getConfig(); cfg != nil {
		excludedIDs = make(map[string]bool, len(cfg.Options.ExcludedBrokers))
		excludedNames = make(map[string]bool, len(cfg.Options.ExcludedBrokers))
		for _, e := range cfg.Options.ExcludedBrokers {
			e = strings.ToLower(e)
			excludedIDs[e] = true
			excludedNames[e] = true
		}
		excludedCats = make(map[string]bool, len(cfg.Options.ExcludedCategories))
		for _, c := range cfg.Options.ExcludedCategories {
			excludedCats[strings.ToLower(c)] = true
		}
	}

	search = strings.ToLower(strings.TrimSpace(search))
	category = strings.ToLower(strings.TrimSpace(category))
	region = strings.ToLower(strings.TrimSpace(region))
	statusFilter = strings.ToLower(strings.TrimSpace(statusFilter))

	manualMode := false
	if cfg := s.getConfig(); cfg != nil {
		manualMode = cfg.IsManualSend()
	}

	var result []BrokerWithStatus
	for _, b := range s.brokerDB.Brokers {
		excluded := excludedIDs[strings.ToLower(b.ID)] || excludedNames[strings.ToLower(b.Name)] || excludedCats[strings.ToLower(b.Category)]
		if excluded && !showExcluded {
			continue
		}

		// Search filter
		if search != "" {
			name := strings.ToLower(b.Name)
			email := strings.ToLower(b.Email)
			if !strings.Contains(name, search) && !strings.Contains(email, search) {
				continue
			}
		}

		// Category filter
		if category != "" && strings.ToLower(b.Category) != category {
			continue
		}

		// Region filter
		if region != "" && strings.ToLower(b.Region) != region {
			continue
		}

		// Missing-email filter - brokers with no contact address on file,
		// mirrors the CLI's `list-brokers --missing-email`
		if missingEmail && b.Email != "" {
			continue
		}

		bws := BrokerWithStatus{
			Broker:     b,
			Status:     "never",
			Excluded:   excluded,
			ManualMode: manualMode,
		}

		if status, ok := brokerStatuses[b.ID]; ok {
			bws.Status = string(status.Status)
			bws.TotalSent = status.TotalSent
			if !status.LastSent.IsZero() {
				bws.LastSent = status.LastSent.Format("Jan 2, 2006")
			}
		}

		// Status filter - "pending" means never sent
		if statusFilter != "" {
			if statusFilter == "pending" && bws.Status != "never" {
				continue
			} else if statusFilter == "sent" && bws.Status != "sent" {
				continue
			} else if statusFilter == "failed" && bws.Status != "failed" {
				continue
			}
		}

		result = append(result, bws)
	}

	return result
}

func (s *Server) getUniqueValues(getter func(broker.Broker) string) []string {
	seen := make(map[string]bool)
	var vals []string
	for _, b := range s.brokerDB.Brokers {
		if v := getter(b); v != "" && !seen[v] {
			seen[v] = true
			vals = append(vals, v)
		}
	}
	return vals
}

func (s *Server) getUniqueCategories() []string {
	return s.getUniqueValues(func(b broker.Broker) string { return b.Category })
}

func (s *Server) getUniqueRegions() []string {
	return s.getUniqueValues(func(b broker.Broker) string { return b.Region })
}

func (s *Server) getStats(profileID string) Stats {
	stats := Stats{
		TotalBrokers: len(s.brokerDB.Brokers),
	}

	if s.historyStore != nil {
		_, sent, failed, err := s.historyStore.GetStats(profileID)
		if err == nil {
			stats.Sent = sent
			stats.Failed = failed
		}
	}

	stats.Pending = stats.TotalBrokers - stats.Sent - stats.Failed
	if stats.Pending < 0 {
		stats.Pending = 0
	}

	return stats
}

func (s *Server) getRecentHistory(profileID string, limit int) []history.Record {
	if s.historyStore == nil {
		return nil
	}
	records, _ := s.historyStore.GetRecentRequests(profileID, limit)
	return records
}

func (s *Server) renderPartial(w http.ResponseWriter, name string, data interface{}) {
	tmpl, ok := s.templates[name]
	if !ok {
		http.Error(w, "Template not found: "+name, http.StatusInternalServerError)
		return
	}
	// Execute the template directly without layout wrapper
	err := tmpl.Execute(w, data)
	if err != nil {
		http.Error(w, "Template error: "+err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) renderWithCSRF(w http.ResponseWriter, r *http.Request, name string, data map[string]interface{}) {
	// filippo.io/csrf/gorilla enforces same-origin via Sec-Fetch-Site and
	// ignores tokens entirely, so there is nothing meaningful to put here.
	// Keep the keys populated (empty) so the form templates' {{.CSRFField}} /
	// {{.CSRFToken}} keep rendering without a change, and any HTMX code that
	// reads the meta tag still finds it.
	data["CSRFToken"] = ""
	data["CSRFField"] = template.HTML("")

	// Every page gets the profile switcher's data, regardless of whether the
	// handler itself needed the active profile - Profiles has length 1 for a
	// single-profile config, in which case layout.html hides the switcher.
	// Profiles/ActiveProfile must always be set, even when cfg is nil (a
	// fresh install with no config.yaml yet, e.g. the very first /setup
	// page) - leaving the map key entirely absent used to make layout.html's
	// `{{len .Profiles}}` fail with "error calling len: reflect: call of
	// reflect.Value.Type on zero Value" on every brand-new install.
	data["Profiles"] = []config.NamedProfile{}
	data["ActiveProfile"] = config.NamedProfile{}
	data["CurrentPath"] = r.URL.Path
	data["Version"] = Version
	if cfg := s.getConfig(); cfg != nil {
		data["Profiles"] = cfg.GetProfiles()
		data["ActiveProfile"] = s.activeProfile(r)
	}

	tmpl, ok := s.templates[name]
	if !ok {
		http.Error(w, "Template not found: "+name, http.StatusInternalServerError)
		return
	}
	err := tmpl.ExecuteTemplate(w, "layout", data)
	if err != nil {
		http.Error(w, "Template error: "+err.Error(), http.StatusInternalServerError)
	}
}
