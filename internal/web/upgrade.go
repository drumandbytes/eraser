package web

import (
	"net/http"
	"os"
)

// upgradeNotice says a 1.x config was migrated on load, while its backup is
// still around and the person hasn't dismissed the notice. The backup file is
// the signal: nothing else is stored.
type upgradeNotice struct {
	Backup string
}

func (s *Server) upgradeNotice() *upgradeNotice {
	if s.configPath == "" {
		return nil
	}
	bak := s.configPath + ".1x.bak"
	if _, err := os.Stat(bak); err != nil {
		return nil
	}
	if _, err := os.Stat(s.configPath + ".1x.notice-seen"); err == nil {
		return nil
	}
	return &upgradeNotice{Backup: bak}
}

func (s *Server) handleDismissUpgradeNotice(w http.ResponseWriter, r *http.Request) {
	if s.configPath != "" {
		_ = os.WriteFile(s.configPath+".1x.notice-seen", nil, 0o600)
	}
	back := "/"
	if ref := r.Header.Get("Referer"); ref != "" {
		if u, err := r.URL.Parse(ref); err == nil && u.Path != "" {
			back = u.Path
		}
	}
	http.Redirect(w, r, back, http.StatusSeeOther)
}
