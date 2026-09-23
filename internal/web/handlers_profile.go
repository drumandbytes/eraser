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

// buildProfileFromForm parses and validates the profile-form fields shared
// by the setup wizard (handleSetupProfile) and this "add profile" settings
// form: first/middle/last name, email, and the optional address fields.
// Returns the parsed profile and a field->message map of validation errors
// (empty if valid) - factored out so the two handlers can't drift on what
// "a valid profile" means, the way they previously did as two independent
// copies of the same three checks.
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

// buildMailOverrideFromForm parses the optional "dedicated email account"
// fields shared by the add/edit profile forms (mail_email/mail_password) -
// the web equivalent of `eraser profile add/edit`'s mail-override prompt
// (see promptMailOverride in cmd/eraser/cmd_profile.go and
// docs/multi-profile.md#per-profile-email-accounts). A blank mail_email
// means "no override" (nil, matching NamedProfile.Mail's zero value) - it's
// also how an existing override gets removed by clearing the field.
// existingAddr/existingPassword let a blank mail_password on an edit keep
// the already-stored app password rather than blanking it out, the same
// blank-to-keep pattern cmd_profile.go's promptSecretWithDefault uses - but
// only when the address wasn't also changed, since the old password almost
// certainly doesn't belong to a newly-typed address.
func buildMailOverrideFromForm(r *http.Request, existingAddr, existingPassword string) (*config.MailConfig, map[string]string) {
	addr := strings.TrimSpace(r.FormValue("mail_email"))
	password := r.FormValue("mail_password")

	errors := make(map[string]string)
	if addr == "" {
		if password != "" {
			errors["mail_email"] = "Enter the Gmail address this app password belongs to"
		}
		return nil, errors
	}

	if err := email.ValidateEmail(addr); err != nil {
		errors["mail_email"] = "Please enter a valid email address"
	}
	if password == "" && existingAddr != "" && strings.EqualFold(addr, existingAddr) {
		password = existingPassword
	}
	if password == "" {
		errors["mail_password"] = "App password is required"
	}
	if len(errors) > 0 {
		return nil, errors
	}

	return &config.MailConfig{
		Email: &config.EmailConfig{
			Provider: "smtp",
			From:     addr,
			SMTP: config.SMTPConfig{
				Host:     "smtp.gmail.com",
				Port:     465,
				UseTLS:   true,
				Username: addr,
				Password: password,
			},
		},
		Inbox: &config.InboxConfig{
			Enabled:  true,
			Provider: "gmail",
			Email:    addr,
			Password: password,
		},
	}, errors
}

// handleSettingsProfileNew adds a second (or third, ...) named profile from
// the web UI - previously only possible via `eraser profile add` on the
// CLI. Collects the same core fields the setup wizard's profile step does,
// plus the optional dedicated-email-account fields (see
// buildMailOverrideFromForm).
func (s *Server) handleSettingsProfileNew(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" {
		limitFormBody(w, r)
		profile, errors := buildProfileFromForm(r)
		mail, mailErrors := buildMailOverrideFromForm(r, "", "")
		for k, v := range mailErrors {
			errors[k] = v
		}

		if len(errors) > 0 {
			s.renderWithCSRF(w, r, "settings/profile-new.html", map[string]interface{}{
				"Title":     "Add Profile",
				"Profile":   profile,
				"Errors":    errors,
				"MailEmail": r.FormValue("mail_email"),
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
				"Title":     "Add Profile",
				"Profile":   profile,
				"Errors":    map[string]string{"_": "Failed to save configuration: " + err.Error()},
				"MailEmail": r.FormValue("mail_email"),
			})
			return
		}
		s.config.Store(&newCfg)

		http.Redirect(w, r, "/settings", http.StatusSeeOther)
		return
	}

	s.renderWithCSRF(w, r, "settings/profile-new.html", map[string]interface{}{
		"Title":     "Add Profile",
		"Profile":   config.Profile{},
		"Errors":    map[string]string{},
		"MailEmail": "",
	})
}

// handleSettingsProfileEdit edits an existing profile's fields. The
// profile's ID itself is never changed here - NamedProfile.ID is stored
// verbatim in history.db, so changing it would orphan that profile's
// existing send history - only its Profile fields (name, email, address...)
// are updated.
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

	existingMailAddr, existingMailPassword := "", ""
	if existing.Mail != nil && existing.Mail.Email != nil {
		existingMailAddr = existing.Mail.Email.From
		existingMailPassword = existing.Mail.Email.SMTP.Password
	}

	if r.Method == "POST" {
		limitFormBody(w, r)
		profile, errors := buildProfileFromForm(r)
		mail, mailErrors := buildMailOverrideFromForm(r, existingMailAddr, existingMailPassword)
		for k, v := range mailErrors {
			errors[k] = v
		}

		if len(errors) > 0 {
			s.renderWithCSRF(w, r, "settings/profile-edit.html", map[string]interface{}{
				"Title":          "Edit Profile",
				"ProfileID":      id,
				"Profile":        profile,
				"Errors":         errors,
				"MailEmail":      r.FormValue("mail_email"),
				"MailConfigured": existingMailAddr != "",
			})
			return
		}

		newCfg := *cfg
		if len(cfg.Profiles) > 0 {
			updated := make([]config.NamedProfile, len(cfg.Profiles))
			copy(updated, cfg.Profiles)
			found := false
			for i, p := range updated {
				if strings.EqualFold(p.ID, existing.ID) {
					updated[i].Profile = profile
					updated[i].Mail = mail
					found = true
					break
				}
			}
			if !found {
				http.Error(w, "Profile not found", http.StatusNotFound)
				return
			}
			newCfg.Profiles = updated
		} else {
			// Legacy single-profile mode (no profiles: list yet) - write
			// back to the top-level profile: block rather than promoting to
			// a profiles: list just because it was edited. A dedicated mail
			// account is a NamedProfile-only concept (it exists to tell
			// several profiles' accounts apart), so it's a no-op here - the
			// lone profile already has the top-level email:/inbox: blocks
			// to itself.
			newCfg.Profile = profile
		}

		if err := config.Save(s.configPath, &newCfg); err != nil {
			s.renderWithCSRF(w, r, "settings/profile-edit.html", map[string]interface{}{
				"Title":          "Edit Profile",
				"ProfileID":      id,
				"Profile":        profile,
				"Errors":         map[string]string{"_": "Failed to save configuration: " + err.Error()},
				"MailEmail":      r.FormValue("mail_email"),
				"MailConfigured": existingMailAddr != "",
			})
			return
		}
		s.config.Store(&newCfg)

		http.Redirect(w, r, "/settings", http.StatusSeeOther)
		return
	}

	s.renderWithCSRF(w, r, "settings/profile-edit.html", map[string]interface{}{
		"Title":          "Edit Profile",
		"ProfileID":      existing.ID,
		"Profile":        existing.Profile,
		"Errors":         map[string]string{},
		"MailEmail":      existingMailAddr,
		"MailConfigured": existingMailAddr != "",
	})
}

// handleSettingsProfileDelete removes a profile from the profiles: list.
// It never deletes that profile's send history - removal_requests rows
// stay in history.db tagged with the now-orphaned profile ID, and become
// visible again if a profile with the same ID is re-added later. Refuses
// to remove the only configured profile, since every profile-scoped
// handler assumes there's always at least one.
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
