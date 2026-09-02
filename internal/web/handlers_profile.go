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

// handleSettingsProfileNew adds a second (or third, ...) named profile from
// the web UI - previously only possible via `eraser profile add` on the
// CLI. Only collects the same core fields the setup wizard's profile step
// does; email/SMTP configuration is shared across all profiles, so there's
// nothing else to ask for here.
func (s *Server) handleSettingsProfileNew(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" {
		limitFormBody(w, r)
		profile, errors := buildProfileFromForm(r)

		if len(errors) > 0 {
			s.renderWithCSRF(w, r, "settings/profile-new.html", map[string]interface{}{
				"Title":   "Add Profile",
				"Profile": profile,
				"Errors":  errors,
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
		})

		if err := config.Save(s.configPath, &newCfg); err != nil {
			s.renderWithCSRF(w, r, "settings/profile-new.html", map[string]interface{}{
				"Title":   "Add Profile",
				"Profile": profile,
				"Errors":  map[string]string{"_": "Failed to save configuration: " + err.Error()},
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

	if r.Method == "POST" {
		limitFormBody(w, r)
		profile, errors := buildProfileFromForm(r)

		if len(errors) > 0 {
			s.renderWithCSRF(w, r, "settings/profile-edit.html", map[string]interface{}{
				"Title":     "Edit Profile",
				"ProfileID": id,
				"Profile":   profile,
				"Errors":    errors,
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
			// a profiles: list just because it was edited.
			newCfg.Profile = profile
		}

		if err := config.Save(s.configPath, &newCfg); err != nil {
			s.renderWithCSRF(w, r, "settings/profile-edit.html", map[string]interface{}{
				"Title":     "Edit Profile",
				"ProfileID": id,
				"Profile":   profile,
				"Errors":    map[string]string{"_": "Failed to save configuration: " + err.Error()},
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
