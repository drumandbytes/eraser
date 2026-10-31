package web

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/drumandbytes/eraser/internal/broker"
	"github.com/drumandbytes/eraser/internal/config"
	"github.com/drumandbytes/eraser/internal/schedule"
	emaTemplate "github.com/drumandbytes/eraser/internal/template"
)

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	s.renderSettings(w, r, nil)
}

// automationView is what the Settings "Automation" card shows.
type automationView struct {
	Supported bool // OS scheduler available on this platform
	Installed bool // OS job set up
	Enabled   bool // in-app scheduler turned on (schedule.enabled)
	Running   bool // a cycle holds the lock right now
	Every     string
	Last      *schedule.State
	LastVia   string
	Next      time.Time // zero when nothing is scheduled
	Message   string
	Success   bool
}

func (s *Server) automationView() automationView {
	v := automationView{
		Supported: schedule.Supported(),
		Installed: s.osInstalled(),
		Running:   s.cycleInProgress(),
		Every:     schedule.Every,
	}
	if cfg := s.getConfig(); cfg != nil {
		v.Enabled = cfg.Schedule.Enabled
	}
	if st, err := schedule.LoadState(s.dataDir); err == nil && st != nil {
		v.Last = st
		v.LastVia = map[string]string{"os": "OS scheduler", "serve": "this web app", "once": "eraser auto --once", "loop": "eraser auto"}[st.Mode]
		if v.Enabled && !v.Installed {
			v.Next = st.LastRun.Add(schedule.Interval)
		}
	}
	if v.Installed {
		v.Next = schedule.NextOSRun(time.Now())
	}
	return v
}

// handleSettingsAutomation handles the Automation card's buttons. These act
// on every profile, not just the active one: a cycle sends for all of them.
func (s *Server) handleSettingsAutomation(w http.ResponseWriter, r *http.Request) {
	limitFormBody(w, r)
	if err := r.ParseForm(); err != nil {
		s.renderAutomationMessage(w, r, "Failed to parse form", false)
		return
	}

	switch r.FormValue("action") {
	case "enable", "disable":
		cfg := s.getConfig()
		if cfg == nil {
			s.renderAutomationMessage(w, r, "Finish setup before turning on automation.", false)
			return
		}
		newCfg := *cfg
		newCfg.Schedule.Enabled = r.FormValue("action") == "enable"
		if err := s.saveConfig(&newCfg); err != nil {
			s.renderAutomationMessage(w, r, "Failed to save configuration: "+err.Error(), false)
			return
		}
		s.config.Store(&newCfg)
		if newCfg.Schedule.Enabled {
			s.renderAutomationMessage(w, r, "Automation is on. Eraser runs "+schedule.Every+" while this web app is open.", true)
		} else {
			s.renderAutomationMessage(w, r, "In-app automation is off.", true)
		}

	case "install":
		cfg := s.getConfig()
		if cfg == nil {
			s.renderAutomationMessage(w, r, "Finish setup before scheduling.", false)
			return
		}
		if !schedule.Supported() {
			s.renderAutomationMessage(w, r, "Your OS has no supported scheduler. Use the in-app option instead.", false)
			return
		}
		job, err := schedule.NewJob(cfg, s.configPath)
		if err == nil {
			err = s.installOS(job)
		}
		if err != nil {
			s.renderAutomationMessage(w, r, "Couldn't install the scheduled job: "+err.Error(), false)
			return
		}
		s.renderAutomationMessage(w, r, "Scheduled. Your OS now runs Eraser "+schedule.Every+", even when this web app is closed.", true)

	case "remove":
		if err := s.removeOS(); err != nil {
			s.renderAutomationMessage(w, r, "Couldn't remove the scheduled job: "+err.Error(), false)
			return
		}
		s.renderAutomationMessage(w, r, "Removed the scheduled job.", true)

	case "run":
		if s.getConfig() == nil {
			s.renderAutomationMessage(w, r, "Finish setup first.", false)
			return
		}
		if s.jobManager.AnyActive() || s.cycleInProgress() || !s.startCycle() {
			s.renderAutomationMessage(w, r, "Something is already sending. Try again once it finishes.", false)
			return
		}
		s.renderAutomationMessage(w, r, "Started a run. Refresh in a minute to see the result.", true)

	default:
		s.renderAutomationMessage(w, r, "Unknown action", false)
	}
}

func (s *Server) renderAutomationMessage(w http.ResponseWriter, r *http.Request, message string, success bool) {
	v := s.automationView()
	v.Message, v.Success = message, success
	s.renderSettings(w, r, map[string]interface{}{"Automation": v})
}

func (s *Server) handleSettingsInbox(w http.ResponseWriter, r *http.Request) {
	limitFormBody(w, r)
	if err := r.ParseForm(); err != nil {
		s.renderSettingsWithMessage(w, r, "Failed to parse form", false)
		return
	}

	cfg := s.getConfig()
	if cfg == nil {
		cfg = &config.Config{}
	}
	form := readMailForm(r)
	if form.Password == "" && strings.EqualFold(form.Username, cfg.Inbox.Email) {
		form.Password = cfg.Inbox.Password
	}
	errors := form.validate("imap", true)
	if len(errors) > 0 {
		s.renderSettings(w, r, map[string]interface{}{
			"InboxMessage": "Please fix the highlighted fields",
			"InboxSuccess": false,
			"InboxForm":    newMailFormView("imap", form, errors),
		})
		return
	}

	// Load-copy-mutate-store rather than mutating the struct returned by
	// getConfig() in place - a concurrent reader (another handler, or a
	// background send-job goroutine) may be holding that exact pointer.
	newCfg := *cfg
	inbox := *form.inboxConfig()
	// keep hand-tuned folder settings; this form only sets the account
	inbox.Folder, inbox.AutoArchive, inbox.ArchiveFolder = cfg.Inbox.Folder, cfg.Inbox.AutoArchive, cfg.Inbox.ArchiveFolder
	config.ApplyInboxDefaults(&inbox)
	newCfg.Inbox = inbox

	if err := s.saveConfig(&newCfg); err != nil {
		s.renderSettingsWithMessage(w, r, "Failed to save configuration: "+err.Error(), false)
		return
	}

	s.config.Store(&newCfg)

	s.renderSettingsWithMessage(w, r, "Inbox monitoring enabled successfully!", true)
}

// requestTemplates are the built-in email templates (internal/template/templates).
var requestTemplates = []string{"gdpr", "ccpa", "generic"}

var knownRegions = []string{"us", "eu", "global"}

type regionChoice struct {
	Name    string
	Checked bool
}

func regionChoices(cfg *config.Config) []regionChoice {
	out := make([]regionChoice, len(knownRegions))
	for i, n := range knownRegions {
		out[i] = regionChoice{Name: n}
		if cfg != nil {
			out[i].Checked = slices.Contains(cfg.Options.Regions, n)
		}
	}
	return out
}

func (s *Server) handleSettingsOptions(w http.ResponseWriter, r *http.Request) {
	limitFormBody(w, r)
	fail := func(msg string) {
		s.renderSettings(w, r, map[string]interface{}{"OptionsMessage": msg, "OptionsSuccess": false})
	}
	if err := r.ParseForm(); err != nil {
		fail("Failed to parse form")
		return
	}
	cfg := s.getConfig()
	if cfg == nil {
		fail("No configuration found - run setup first")
		return
	}
	custom := strings.TrimSpace(strings.ReplaceAll(r.FormValue("custom_template"), "\r\n", "\n"))
	tmpl := r.FormValue("template")
	if tmpl == emaTemplate.CustomName {
		if custom == "" {
			fail("Write the custom template text first, or pick a built-in template")
			return
		}
	} else if !slices.Contains(requestTemplates, tmpl) {
		fail("Unknown template")
		return
	}
	if custom != "" {
		if err := emaTemplate.CheckCustom(custom); err != nil {
			fail("Custom template: " + err.Error())
			return
		}
	}
	rate, err := strconv.Atoi(strings.TrimSpace(r.FormValue("rate_limit_ms")))
	if err != nil || rate < 0 {
		fail("Rate limit must be a non-negative number")
		return
	}
	daily, err := strconv.Atoi(strings.TrimSpace(r.FormValue("daily_send_limit")))
	if err != nil || daily < 1 {
		fail("Daily send limit must be a positive number")
		return
	}
	list := r.FormValue("broker_list")
	if list != "full" && list != "verified" {
		fail("Broker list must be full or verified")
		return
	}
	if list == "full" {
		list = ""
	}
	var regions []string
	for _, reg := range r.Form["regions"] {
		if !slices.Contains(knownRegions, reg) {
			fail("Unknown region")
			return
		}
		regions = append(regions, reg)
	}
	var cats []string
	for _, c := range strings.Split(r.FormValue("excluded_categories"), ",") {
		if c = strings.ToLower(strings.TrimSpace(c)); c != "" {
			cats = append(cats, c)
		}
	}
	sendMode := ""
	if r.FormValue("manual_send") != "" {
		sendMode = "manual"
	} else if cfg.IsManualSend() && !cfg.Email.Configured() {
		fail("Add your email account in Settings → Email Configuration before turning off manual sending")
		return
	}
	newCfg := *cfg
	o := &newCfg.Options
	o.SendMode = sendMode
	o.Template, o.CustomTemplate, o.RateLimitMs, o.DailySendLimit = tmpl, custom, rate, daily
	o.Regions, o.BrokerList, o.ExcludedCategories = regions, list, cats
	if err := s.saveConfig(&newCfg); err != nil {
		fail("Failed to save configuration: " + err.Error())
		return
	}
	s.config.Store(&newCfg)
	_ = s.tmplEngine.SetCustom(custom) // already validated by CheckCustom
	if list != cfg.Options.BrokerList {
		if err := s.reloadBrokers(); err != nil {
			fail("Saved, but reloading the broker list failed: " + err.Error())
			return
		}
	}
	s.renderSettings(w, r, map[string]interface{}{"OptionsMessage": "Request options saved.", "OptionsSuccess": true})
}

func (s *Server) renderSettingsWithMessage(w http.ResponseWriter, r *http.Request, message string, success bool) {
	s.renderSettings(w, r, map[string]interface{}{
		"InboxMessage": message,
		"InboxSuccess": success,
	})
}

// renderSettings renders settings.html with the shared page data plus extra.
func (s *Server) renderSettings(w http.ResponseWriter, r *http.Request, extra map[string]interface{}) {
	cfg := s.getConfig()
	data := map[string]interface{}{
		"Title":       "Settings",
		"Config":      cfg,
		"Automation":  s.automationView(),
		"InboxForm":   inboxFormView(cfg),
		"BrokerCount": len(s.brokers().Brokers),
		"OwnBrokers":  ownBrokerCount(),
		"Templates":   requestTemplates,
		"Regions":     regionChoices(cfg),
		"Passphrase":  s.passphraseView(cfg),
	}
	for k, v := range extra {
		data[k] = v
	}
	s.renderWithCSRF(w, r, "settings.html", data)
}

// inboxFormView prefills the inbox form from the saved inbox, else from the
// sending account (same provider, profile address).
func inboxFormView(cfg *config.Config) mailFormView {
	if cfg == nil {
		return newMailFormView("imap", mailForm{}, nil)
	}
	f := mailForm{Address: cfg.PrimaryProfile().Email, Provider: config.ProviderIDForHosts(cfg.Email.SMTP.Host, "")}
	if in := cfg.Inbox; in.Email != "" {
		f = mailForm{Address: in.Email, Username: in.Email, IMAPHost: in.Server, IMAPPort: in.Port}
		if _, ok := config.ProviderByID(in.Provider); ok {
			f.Provider = in.Provider
		}
	}
	v := newMailFormView("imap", f, nil)
	if cfg.Inbox.Password != "" {
		v.PasswordPlaceholder = "Leave blank to keep current"
	}
	return v
}

// handleSettingsBrokersUpdate is `eraser update-brokers`: fetch the published
// list into ~/.eraser/brokers.yaml, then reload the list this server uses.
func (s *Server) handleSettingsBrokersUpdate(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	res, err := broker.Update(ctx, s.brokerUpdateURL, false)
	if err != nil {
		s.renderBrokersMessage(w, r, "Update failed: "+err.Error(), false)
		return
	}
	if !res.Changed {
		s.renderBrokersMessage(w, r, "The broker list is already up to date.", true)
		return
	}

	if err := s.reloadBrokers(); err != nil {
		s.renderBrokersMessage(w, r, "Downloaded, but reloading failed: "+err.Error(), false)
		return
	}
	var opts config.Options
	if cfg := s.getConfig(); cfg != nil {
		opts = cfg.Options
	}

	msg := fmt.Sprintf("Broker list updated: %d entries (was %d).", res.Count, res.Before)
	if res.Own > 0 {
		msg += fmt.Sprintf(" Your %d own broker(s) are kept.", res.Own)
	}
	if s.BrokerOverride != "" || opts.BrokerFile != "" || strings.EqualFold(opts.BrokerList, "verified") {
		msg += " Your setup sends to a different list (--brokers, broker_file or the verified list), so the brokers used for sending didn't change."
	}
	s.renderBrokersMessage(w, r, msg, true)
}

func (s *Server) renderBrokersMessage(w http.ResponseWriter, r *http.Request, message string, success bool) {
	s.renderSettings(w, r, map[string]interface{}{
		"BrokersMessage": message,
		"BrokersSuccess": success,
	})
}

// reloadBrokers re-resolves the broker list the way serve did at startup
// (including the user's own entries) and swaps it in.
func (s *Server) reloadBrokers() error {
	var opts config.Options
	if cfg := s.getConfig(); cfg != nil {
		opts = cfg.Options
	}
	db, err := broker.LoadList(s.BrokerOverride, opts.BrokerFile, opts.BrokerList)
	if err != nil {
		return err
	}
	s.brokerDB.Store(db)
	return nil
}

func ownBrokerCount() int {
	local, err := broker.LoadLocal()
	if err != nil {
		return 0
	}
	return len(local.Brokers)
}
