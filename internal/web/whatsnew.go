package web

import (
	"net/http"
	"os"

	"github.com/drumandbytes/eraser/internal/config"
)

// whatsNew is the one-time "What's new in Eraser 2.0" card on the dashboard.
// It also carries the config-upgrade note when a 1.x config was migrated, so
// upgraders see one notice, not two.
type whatsNew struct {
	Upgraded *upgradeNotice
}

func (s *Server) whatsNew() *whatsNew {
	cfg := s.getConfig()
	if s.configPath == "" || cfg == nil || !cfg.HasProfile() || config.WhatsNewSeen(s.configPath) {
		return nil
	}
	return &whatsNew{Upgraded: s.upgradeNotice()}
}

func (s *Server) handleDismissWhatsNew(w http.ResponseWriter, r *http.Request) {
	if s.configPath != "" {
		config.MarkWhatsNewSeen(s.configPath)
		if s.upgradeNotice() != nil { // dismissing the card also settles the migration note
			_ = os.WriteFile(s.configPath+".1x.notice-seen", nil, 0o600)
		}
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
