package web

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/drumandbytes/eraser/internal/broker"
	"github.com/drumandbytes/eraser/internal/config"
	"github.com/drumandbytes/eraser/internal/schedule"
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
		if err := config.Save(s.configPath, &newCfg); err != nil {
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

	if err := config.Save(s.configPath, &newCfg); err != nil {
		s.renderSettingsWithMessage(w, r, "Failed to save configuration: "+err.Error(), false)
		return
	}

	s.config.Store(&newCfg)

	s.renderSettingsWithMessage(w, r, "Inbox monitoring enabled successfully!", true)
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

	var opts config.Options
	if cfg := s.getConfig(); cfg != nil {
		opts = cfg.Options
	}
	db, err := broker.LoadList(s.BrokerOverride, opts.BrokerFile, opts.BrokerList)
	if err != nil {
		s.renderBrokersMessage(w, r, "Downloaded, but reloading failed: "+err.Error(), false)
		return
	}
	s.brokerDB.Store(db)

	msg := fmt.Sprintf("Broker list updated: %d entries (was %d).", res.Count, res.Before)
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
