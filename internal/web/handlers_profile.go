package web

import (
	"net/http"
	"strings"

	"github.com/drumandbytes/eraser/internal/config"
	"github.com/drumandbytes/eraser/internal/email"
	"github.com/go-chi/chi/v5"
)

// handleAPISwitchProfile sets the active-profile cookie (after validating
// the requested ID is actually configured) and redirects back to the page
// the switcher was submitted from, so every profile-scoped page picks up
// the new selection on next render.
func (s *Server) handleAPISwitchProfile(w http.ResponseWriter, r *http.Request) {
	limitFormBody(w, r)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}

	id := r.FormValue("profile_id")
	if cfg := s.getConfig(); cfg != nil {
		for _, p := range cfg.GetProfiles() {
			if p.ID == id {
				http.SetCookie(w, &http.Cookie{
					Name:     activeProfileCookie,
					Value:    id,
					Path:     "/",
					SameSite: http.SameSiteLaxMode,
					MaxAge:   365 * 24 * 60 * 60,
				})
				break
			}
		}
	}

	redirect := r.FormValue("redirect")
	if redirect == "" || !strings.HasPrefix(redirect, "/") || strings.HasPrefix(redirect, "//") {
		redirect = "/"
	}
	http.Redirect(w, r, redirect, http.StatusSeeOther)
}

// buildProfileFromForm parses and validates the profile fields shared by the
// setup wizard and the "add profile" form. Returns field -> error.
func buildProfileFromForm(r *http.Request) (config.Profile, map[string]string) {
	profile := config.Profile{
		FirstName:  strings.TrimSpace(r.FormValue("first_name")),
		MiddleName: strings.TrimSpace(r.FormValue("middle_name")),
		LastName:   strings.TrimSpace(r.FormValue("last_name")),
		Email:      strings.TrimSpace(r.FormValue("email")),
		Address:    strings.TrimSpace(r.FormValue("address")),
		City:       strings.TrimSpace(r.FormValue("city")),
		State:      strings.TrimSpace(r.FormValue("state")),
		ZipCode:    strings.TrimSpace(r.FormValue("zip_code")),
		Country:    strings.TrimSpace(r.FormValue("country")),
		Phone:      strings.TrimSpace(r.FormValue("phone")),
	}

	errors := make(map[string]string)
	if profile.FirstName == "" {
		errors["first_name"] = "First name is required"
	}
	if profile.LastName == "" {
		errors["last_name"] = "Last name is required"
	}
	if profile.Email == "" {
		errors["email"] = "Email is required"
	} else if err := email.ValidateEmail(profile.Email); err != nil {
		errors["email"] = "Please enter a valid email address"
	}
	return profile, errors
}

// buildMailOverrideFromForm parses the optional dedicated account
// (partials/mail-account.html), like promptMailOverride in the CLI. Blank
// address = no override (also how one is removed). A blank password keeps the
// stored one unless the login changed. Send-only providers (SES) get no inbox
// override, so replies fall back to the shared inbox.
func buildMailOverrideFromForm(r *http.Request, existing *config.MailConfig) (*config.MailConfig, mailFormView, map[string]string) {
	form := readMailForm(r)
	if form.Address == "" {
		errors := map[string]string{}
		if form.Password != "" {
			errors["mail_address"] = "Enter the address this password belongs to"
		}
		return nil, newMailFormView("both", form, errors), errors
	}
	if existing != nil && existing.Email != nil {
		if form.SMTPHost == "" { // no provider posted: keep the saved servers
			saved := mailFormFromConfig(*existing.Email, existing.Inbox)
			form.SMTPHost, form.SMTPPort, form.IMAPHost, form.IMAPPort = saved.SMTPHost, saved.SMTPPort, saved.IMAPHost, saved.IMAPPort
		}
		if form.Password == "" && strings.EqualFold(form.Username, existing.Email.SMTP.Username) {
			form.Password = existing.Email.SMTP.Password
		}
	}
	errors := form.validate("both", true)
	view := newMailFormView("both", form, errors)
	if len(errors) > 0 {
		return nil, view, errors
	}
	e := form.emailConfig()
	return &config.MailConfig{Email: &e, Inbox: form.inboxConfig()}, view, errors
}

// mailOverrideView prefills the dedicated-account fields from a saved override.
func mailOverrideView(m *config.MailConfig) mailFormView {
	if m == nil || m.Email == nil {
		return newMailFormView("both", mailForm{}, nil)
	}
	v := newMailFormView("both", mailFormFromConfig(*m.Email, m.Inbox), nil)
	v.PasswordPlaceholder = "Leave blank to keep current"
	return v
}

// handleSettingsProfileNew adds another named profile, with optional dedicated
// mail account.
func (s *Server) handleSettingsProfileNew(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" {
		limitFormBody(w, r)
		profile, errors := buildProfileFromForm(r)
		mail, mailView, mailErrors := buildMailOverrideFromForm(r, nil)
		for k, v := range mailErrors {
			errors[k] = v
		}

		if len(errors) > 0 {
			s.renderWithCSRF(w, r, "settings/profile-new.html", map[string]interface{}{
				"Title":   "Add Profile",
				"Profile": profile,
				"Errors":  errors,
				"Mail":    mailView,
			})
			return
		}

		cfg := s.getConfig()
		if cfg == nil {
			cfg = &config.Config{}
		}
		newCfg := *cfg
		existing := cfg.GetProfiles()
		newCfg.Profiles = append(append([]config.NamedProfile{}, existing...), config.NamedProfile{
			ID:      config.SlugifyProfileID(profile.FirstName, profile.LastName, existing),
			Profile: profile,
			Mail:    mail,
		})

		if err := config.Save(s.configPath, &newCfg); err != nil {
			s.renderWithCSRF(w, r, "settings/profile-new.html", map[string]interface{}{
				"Title":   "Add Profile",
				"Profile": profile,
				"Errors":  map[string]string{"_": "Failed to save configuration: " + err.Error()},
				"Mail":    mailView,
			})
			return
		}
		s.config.Store(&newCfg)

		http.Redirect(w, r, "/settings", http.StatusSeeOther)
		return
	}

	s.renderWithCSRF(w, r, "settings/profile-new.html", map[string]interface{}{
		"Title":   "Add Profile",
		"Profile": config.Profile{},
		"Errors":  map[string]string{},
		"Mail":    mailOverrideView(nil),
	})
}

// handleSettingsProfileEdit edits a profile's fields. The ID never changes: it's
// stored in history.db.
func (s *Server) handleSettingsProfileEdit(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "profileID")

	cfg := s.getConfig()
	if cfg == nil {
		http.Redirect(w, r, "/settings", http.StatusSeeOther)
		return
	}
	existing, err := cfg.GetProfile(id)
	if err != nil {
		http.Error(w, "Profile not found", http.StatusNotFound)
		return
	}

	if r.Method == "POST" {
		limitFormBody(w, r)
		profile, errors := buildProfileFromForm(r)
		mail, mailView, mailErrors := buildMailOverrideFromForm(r, existing.Mail)
		if existing.Mail != nil {
			mailView.PasswordPlaceholder = "Leave blank to keep current"
		}
		for k, v := range mailErrors {
			errors[k] = v
		}

		if len(errors) > 0 {
			s.renderWithCSRF(w, r, "settings/profile-edit.html", map[string]interface{}{
				"Title":     "Edit Profile",
				"ProfileID": id,
				"Profile":   profile,
				"Errors":    errors,
				"Mail":      mailView,
			})
			return
		}

		newCfg := *cfg
		updated := append([]config.NamedProfile(nil), cfg.GetProfiles()...)
		for i, p := range updated {
			if strings.EqualFold(p.ID, existing.ID) {
				updated[i].Profile = profile
				updated[i].Mail = mail
			}
		}
		newCfg.Profiles, newCfg.Profile = updated, config.Profile{}

		if err := config.Save(s.configPath, &newCfg); err != nil {
			s.renderWithCSRF(w, r, "settings/profile-edit.html", map[string]interface{}{
				"Title":     "Edit Profile",
				"ProfileID": id,
				"Profile":   profile,
				"Errors":    map[string]string{"_": "Failed to save configuration: " + err.Error()},
				"Mail":      mailView,
			})
			return
		}
		s.config.Store(&newCfg)

		http.Redirect(w, r, "/settings", http.StatusSeeOther)
		return
	}

	s.renderWithCSRF(w, r, "settings/profile-edit.html", map[string]interface{}{
		"Title":     "Edit Profile",
		"ProfileID": existing.ID,
		"Profile":   existing.Profile,
		"Errors":    map[string]string{},
		"Mail":      mailOverrideView(existing.Mail),
	})
}

// handleSettingsProfileDelete removes a profile but keeps its history (visible
// again if the ID is re-added). The last profile can't be removed.
func (s *Server) handleSettingsProfileDelete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "profileID")

	cfg := s.getConfig()
	if cfg == nil {
		http.Redirect(w, r, "/settings", http.StatusSeeOther)
		return
	}

	profiles := cfg.GetProfiles()
	if len(profiles) <= 1 {
		http.Error(w, "Can't delete the only configured profile", http.StatusBadRequest)
		return
	}

	remaining := make([]config.NamedProfile, 0, len(profiles)-1)
	found := false
	for _, p := range profiles {
		if strings.EqualFold(p.ID, id) {
			found = true
			continue
		}
		remaining = append(remaining, p)
	}
	if !found {
		http.Error(w, "Profile not found", http.StatusNotFound)
		return
	}

	newCfg := *cfg
	newCfg.Profiles = remaining

	if err := config.Save(s.configPath, &newCfg); err != nil {
		http.Error(w, "Failed to save configuration: "+err.Error(), http.StatusInternalServerError)
		return
	}
	s.config.Store(&newCfg)

	// If the deleted profile was the active one, clear the cookie instead
	// of leaving it pointing at an ID that no longer resolves - activeProfile
	// falls back to the first configured profile once it's gone.
	if cookie, err := r.Cookie(activeProfileCookie); err == nil && strings.EqualFold(cookie.Value, id) {
		http.SetCookie(w, &http.Cookie{
			Name:     activeProfileCookie,
			Value:    "",
			Path:     "/",
			MaxAge:   -1,
			SameSite: http.SameSiteLaxMode,
		})
	}

	http.Redirect(w, r, "/settings", http.StatusSeeOther)
}
