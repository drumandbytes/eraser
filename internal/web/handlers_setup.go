package web

import (
	"context"
	"fmt"
	"html/template"
	"net/http"
	"strings"
	"time"

	"github.com/eraser-privacy/eraser/internal/config"
	"github.com/eraser-privacy/eraser/internal/email"
)

// Setup wizard handlers

func (s *Server) handleSetupWelcome(w http.ResponseWriter, r *http.Request) {
	data := map[string]interface{}{
		"Title": "Setup",
		"Step":  "welcome",
	}
	s.renderWithCSRF(w, r, "setup/welcome.html", data)
}

func (s *Server) handleSetupProfile(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" {
		limitFormBody(w, r)
		profile, errors := buildProfileFromForm(r)
		profile.DateOfBirth = strings.TrimSpace(r.FormValue("dob"))

		if len(errors) > 0 {
			data := map[string]interface{}{
				"Title":   "Setup - Profile",
				"Step":    "profile",
				"Profile": profile,
				"Errors":  errors,
			}
			s.renderWithCSRF(w, r, "setup/profile.html", data)
			return
		}

		// Store profile in secure server-side session (not cookie)
		session := s.getOrCreateSession(w, r)
		if session == nil {
			http.Error(w, "Session error", http.StatusInternalServerError)
			return
		}
		s.updateSession(r, func(sess *Session) {
			sess.Step = "email"
			sess.Profile = profile
		})
		http.Redirect(w, r, "/setup/email", http.StatusFound)
		return
	}

	session := s.getSession(r)
	var profile config.Profile
	if session != nil {
		profile = session.Profile
	}
	data := map[string]interface{}{
		"Title":   "Setup - Profile",
		"Step":    "profile",
		"Profile": profile,
	}
	s.renderWithCSRF(w, r, "setup/profile.html", data)
}

func (s *Server) handleSetupEmail(w http.ResponseWriter, r *http.Request) {
	session := s.getSession(r)

	if session == nil || session.Profile.FirstName == "" {
		http.Redirect(w, r, "/setup/profile", http.StatusFound)
		return
	}

	if r.Method == "POST" {
		limitFormBody(w, r)
		emailCfg := config.Email{
			Provider: "smtp",
			From:     session.Profile.Email,
		}

		errors := make(map[string]string)

		// Parse SMTP configuration (Gmail SMTP)
		emailCfg.SMTP.Host = strings.TrimSpace(r.FormValue("smtp_host"))
		_, _ = fmt.Sscanf(r.FormValue("smtp_port"), "%d", &emailCfg.SMTP.Port)
		emailCfg.SMTP.Username = strings.TrimSpace(r.FormValue("smtp_username"))
		emailCfg.SMTP.Password = strings.TrimSpace(r.FormValue("smtp_password"))
		emailCfg.SMTP.UseTLS = r.FormValue("smtp_tls") == "on"

		// An empty password means "keep the one already in the wizard
		// session" - the form no longer echoes it back into the page, so it
		// arrives blank whenever the user didn't retype it. This is what
		// makes the common "test send failed -> Back to Email Settings ->
		// fix the host -> resubmit" loop work without retyping the app
		// password each time. Sourced from the session (the wizard's own
		// store), not from the saved config file. Applied before validation
		// below, so a first-time setup with no password anywhere still errors.
		if emailCfg.SMTP.Password == "" {
			emailCfg.SMTP.Password = session.Email.SMTP.Password
		}

		// Validate required fields
		if emailCfg.SMTP.Host == "" {
			errors["smtp_host"] = "SMTP host is required"
		}
		if emailCfg.SMTP.Port == 0 {
			errors["smtp_port"] = "SMTP port is required"
		}
		if emailCfg.SMTP.Username == "" {
			errors["smtp_username"] = "Gmail address is required"
		}
		if emailCfg.SMTP.Password == "" {
			errors["smtp_password"] = "App password is required"
		}
		// Enforce TLS when using authentication
		if !emailCfg.SMTP.UseTLS && emailCfg.SMTP.Username != "" {
			errors["smtp_tls"] = "TLS is required for Gmail"
		}

		if len(errors) > 0 {
			data := map[string]interface{}{
				"Title":   "Setup - Gmail",
				"Step":    "email",
				"Profile": session.Profile,
				"Email":   emailCfg,
				"Errors":  errors,
			}
			s.renderWithCSRF(w, r, "setup/email.html", data)
			return
		}

		// Store email config in secure server-side session
		s.updateSession(r, func(sess *Session) {
			sess.Email = emailCfg
			sess.Step = "test"
		})
		http.Redirect(w, r, "/setup/test", http.StatusFound)
		return
	}

	// Set Gmail defaults for new setups
	emailCfg := session.Email
	if emailCfg.SMTP.Host == "" {
		emailCfg.SMTP.Host = "smtp.gmail.com"
		emailCfg.SMTP.Port = 465
		emailCfg.SMTP.UseTLS = true
	}

	data := map[string]interface{}{
		"Title":   "Setup - Gmail",
		"Step":    "email",
		"Profile": session.Profile,
		"Email":   emailCfg,
	}
	s.renderWithCSRF(w, r, "setup/email.html", data)
}

func (s *Server) handleSetupTest(w http.ResponseWriter, r *http.Request) {
	session := s.getSession(r)

	if session == nil || session.Profile.FirstName == "" {
		http.Redirect(w, r, "/setup/profile", http.StatusFound)
		return
	}
	if session.Email.Provider == "" {
		http.Redirect(w, r, "/setup/email", http.StatusFound)
		return
	}

	data := map[string]interface{}{
		"Title":   "Setup - Test",
		"Step":    "test",
		"Profile": session.Profile,
		"Email":   session.Email,
	}
	s.renderWithCSRF(w, r, "setup/test.html", data)
}

func (s *Server) handleSetupTestSend(w http.ResponseWriter, r *http.Request) {
	session := s.getSession(r)

	if session == nil || session.Email.Provider == "" {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`<div class="text-red-600">Email not configured. Please go back to the email step.</div>`))
		return
	}

	// Create email sender with the session config
	sender, err := email.NewSender(session.Email)
	if err != nil {
		_, _ = fmt.Fprintf(w, `
			<div class="bg-red-100 border border-red-400 text-red-700 px-4 py-3 rounded">
				<strong>Configuration error:</strong> %s
				<p class="mt-2 text-sm">Please check your email settings and try again.</p>
			</div>
		`, template.HTMLEscapeString(err.Error()))
		return
	}

	// Send test email
	testMsg := email.Message{
		To:      session.Profile.Email,
		From:    session.Email.From,
		Subject: "Eraser Test Email",
		Body: fmt.Sprintf(`Hello %s,

This is a test email from Eraser to verify your email configuration is working correctly.

If you received this email, your setup is complete and you're ready to start sending data removal requests!

Best,
Eraser`, session.Profile.FirstName),
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	result := sender.Send(ctx, testMsg)
	if !result.Success {
		errMsg := "Unknown error"
		if result.Error != nil {
			errMsg = result.Error.Error()
		}
		_, _ = fmt.Fprintf(w, `
			<div class="bg-red-100 border border-red-400 text-red-700 px-4 py-3 rounded">
				<strong>Test failed:</strong> %s
				<p class="mt-2 text-sm">Please check your email configuration and try again.</p>
			</div>
			<div class="mt-4">
				<a href="/setup/email" class="text-indigo-600 hover:text-indigo-800 font-medium">
					Back to Email Settings
				</a>
			</div>
		`, template.HTMLEscapeString(errMsg))
		return
	}

	_, _ = w.Write([]byte(`
		<div class="bg-green-100 border border-green-400 text-green-700 px-4 py-3 rounded">
			<strong>Success!</strong> Test email sent to your address.
			<p class="mt-2 text-sm">Check your inbox (and spam folder) for the test message.</p>
		</div>
		<div class="mt-4">
			<a href="/setup/complete" class="inline-flex items-center px-6 py-3 bg-indigo-600 text-white font-medium rounded-md hover:bg-indigo-700">
				Complete Setup
			</a>
		</div>
	`))
}

func (s *Server) handleSetupComplete(w http.ResponseWriter, r *http.Request) {
	session := s.getSession(r)

	if session == nil || session.Profile.FirstName == "" || session.Email.Provider == "" {
		http.Redirect(w, r, "/setup", http.StatusFound)
		return
	}

	// Start from the existing config rather than a blank struct. The wizard
	// only ever collects Profile and Email, but Config also carries Profiles,
	// Inbox, Pipeline and the rest of Options - building a fresh struct here
	// silently discarded all of them, so re-running the wizard on a
	// configured install wiped the saved inbox app password, any additional
	// household profiles, and every excluded-broker/region setting.
	cfg := &config.Config{}
	if existing := s.getConfig(); existing != nil {
		*cfg = *existing
	}
	cfg.Profile = session.Profile
	cfg.Email = session.Email

	// Fill defaults only where nothing is configured yet, so a re-run keeps
	// whatever the user already chose.
	if cfg.Options.Template == "" {
		// This fork is customized for GDPR Article 17 use (see EU-NOTES.md) -
		// default fresh setups to gdpr, not upstream's US/CCPA-oriented
		// "generic". Matches the CLI's `init` default.
		cfg.Options.Template = "gdpr"
	}
	if cfg.Options.RateLimitMs == 0 {
		cfg.Options.RateLimitMs = 2000
	}

	if err := config.Save(s.configPath, cfg); err != nil {
		data := map[string]interface{}{
			"Title": "Setup - Error",
			"Error": err.Error(),
		}
		s.renderWithCSRF(w, r, "setup/complete.html", data)
		return
	}

	// Update server's config reference
	s.config.Store(cfg)

	// Clear session - credentials are now saved to config file
	s.clearSession(w, r)

	data := map[string]interface{}{
		"Title":   "Setup Complete",
		"Step":    "complete",
		"Profile": session.Profile,
	}
	s.renderWithCSRF(w, r, "setup/complete.html", data)
}

// Secure session helpers - credentials stored server-side only
// Cookie contains only an opaque session ID, never credentials

func (s *Server) getOrCreateSession(w http.ResponseWriter, r *http.Request) *Session {
	// Check for existing session
	cookie, err := r.Cookie("eraser_session")
	if err == nil && cookie.Value != "" {
		session := s.sessions.Get(cookie.Value)
		if session != nil {
			return session
		}
	}

	// Create new session
	sessionID, err := s.sessions.Create()
	if err != nil {
		return nil
	}

	// Set secure session cookie (ID only, no credentials)
	http.SetCookie(w, &http.Cookie{
		Name:     "eraser_session",
		Value:    sessionID,
		Path:     "/",
		MaxAge:   1800, // 30 minutes
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		// Note: Secure flag omitted for localhost HTTP; add for production HTTPS
	})

	return s.sessions.Get(sessionID)
}

func (s *Server) getSession(r *http.Request) *Session {
	cookie, err := r.Cookie("eraser_session")
	if err != nil || cookie.Value == "" {
		return nil
	}
	return s.sessions.Get(cookie.Value)
}

func (s *Server) updateSession(r *http.Request, updateFn func(*Session)) bool {
	cookie, err := r.Cookie("eraser_session")
	if err != nil || cookie.Value == "" {
		return false
	}
	return s.sessions.Update(cookie.Value, updateFn)
}

func (s *Server) clearSession(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("eraser_session")
	if err == nil && cookie.Value != "" {
		s.sessions.Delete(cookie.Value)
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "eraser_session",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
}
