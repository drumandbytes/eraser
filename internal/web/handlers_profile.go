package web

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/eraser-privacy/eraser/internal/config"
	"github.com/eraser-privacy/eraser/internal/email"
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

var nonSlugChars = regexp.MustCompile(`[^a-z0-9]+`)

// slugifyProfileID turns a name into a short, hyphenated ID in the same
// style as config.NamedProfile.ID's doc comment asks for (e.g. "spouse",
// "kid1") - "Jane Doe" becomes "jane-doe" - and appends -2, -3, ... if
// that's already taken by an existing profile, so the web UI's "add
// profile" form never needs to ask the user to pick one themselves.
func slugifyProfileID(firstName, lastName string, existing []config.NamedProfile) string {
	base := nonSlugChars.ReplaceAllString(strings.ToLower(strings.TrimSpace(firstName+"-"+lastName)), "-")
	base = strings.Trim(base, "-")
	if base == "" {
		base = "profile"
	}

	taken := make(map[string]bool, len(existing))
	for _, p := range existing {
		taken[strings.ToLower(p.ID)] = true
	}

	id := base
	for n := 2; taken[id]; n++ {
		id = fmt.Sprintf("%s-%d", base, n)
	}
	return id
}

// handleSettingsProfileNew adds a second (or third, ...) named profile from
// the web UI - previously only possible via `eraser profile add` on the
// CLI. Only collects the same core fields the setup wizard's profile step
// does; email/SMTP configuration is shared across all profiles, so there's
// nothing else to ask for here.
func (s *Server) handleSettingsProfileNew(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" {
		limitFormBody(w, r)
		profile := config.Profile{
			FirstName: strings.TrimSpace(r.FormValue("first_name")),
			LastName:  strings.TrimSpace(r.FormValue("last_name")),
			Email:     strings.TrimSpace(r.FormValue("email")),
			Address:   strings.TrimSpace(r.FormValue("address")),
			City:      strings.TrimSpace(r.FormValue("city")),
			State:     strings.TrimSpace(r.FormValue("state")),
			ZipCode:   strings.TrimSpace(r.FormValue("zip_code")),
			Country:   strings.TrimSpace(r.FormValue("country")),
			Phone:     strings.TrimSpace(r.FormValue("phone")),
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
			ID:      slugifyProfileID(profile.FirstName, profile.LastName, existing),
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
