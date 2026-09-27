package web

import (
	"net/http"
	"time"

	"github.com/drumandbytes/eraser/internal/config"
	"github.com/drumandbytes/eraser/internal/schedule"
)

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	data := map[string]interface{}{
		"Title":      "Settings",
		"Config":     s.getConfig(),
		"Automation": s.automationView(),
	}
	s.renderWithCSRF(w, r, "settings.html", data)
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
	data := map[string]interface{}{
		"Title":      "Settings",
		"Config":     s.getConfig(),
		"Automation": v,
	}
	s.renderWithCSRF(w, r, "settings.html", data)
}

func (s *Server) handleSettingsInbox(w http.ResponseWriter, r *http.Request) {
	limitFormBody(w, r)
	if err := r.ParseForm(); err != nil {
		s.renderSettingsWithMessage(w, r, "Failed to parse form", false)
		return
	}

	email := r.FormValue("inbox_email")
	password := r.FormValue("inbox_password")

	if email == "" || password == "" {
		s.renderSettingsWithMessage(w, r, "Email and password are required", false)
		return
	}

	// Update config with inbox settings. Load-copy-mutate-store rather than
	// mutating the struct returned by getConfig() in place - a concurrent
	// reader (another handler, or a background send-job goroutine) may be
	// holding that exact pointer.
	cfg := s.getConfig()
	if cfg == nil {
		cfg = &config.Config{}
	}
	newCfg := *cfg

	// start from the existing inbox so a hand-configured provider keeps its
	// server/port/archive; this form only sets email and password
	inbox := newCfg.Inbox
	inbox.Enabled = true
	inbox.Email = email
	inbox.Password = password
	// Set Gmail server/port explicitly: this goes straight into the live
	// config, and config.Load's defaults only apply at startup (else "dial tcp :0").
	if inbox.Provider == "" || inbox.Provider == "gmail" {
		inbox.Provider = "gmail"
		inbox.Server = "imap.gmail.com"
		inbox.Port = 993
	}
	if inbox.Folder == "" {
		inbox.Folder = "INBOX"
	}
	if inbox.ArchiveFolder == "" {
		inbox.ArchiveFolder = "Eraser"
	}
	newCfg.Inbox = inbox

	if err := config.Save(s.configPath, &newCfg); err != nil {
		s.renderSettingsWithMessage(w, r, "Failed to save configuration: "+err.Error(), false)
		return
	}

	s.config.Store(&newCfg)

	s.renderSettingsWithMessage(w, r, "Inbox monitoring enabled successfully!", true)
}

func (s *Server) renderSettingsWithMessage(w http.ResponseWriter, r *http.Request, message string, success bool) {
	data := map[string]interface{}{
		"Title":        "Settings",
		"Config":       s.getConfig(),
		"Automation":   s.automationView(),
		"InboxMessage": message,
		"InboxSuccess": success,
	}
	s.renderWithCSRF(w, r, "settings.html", data)
}
