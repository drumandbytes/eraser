package web

import (
	"net/http"

	"github.com/drumandbytes/delistd/internal/config"
)

// renameNotice reports whether the dashboard shows the one-time "Eraser is now
// Delistd" card. The 2.0 card, when it's up, already holds the upgrader's attention.
func (s *Server) renameNotice() bool {
	cfg := s.getConfig()
	if s.configPath == "" || cfg == nil || !cfg.HasProfile() || config.RenameNoticeSeen(s.configPath) {
		return false
	}
	return config.WhatsNewSeen(s.configPath)
}

func (s *Server) handleDismissRenamed(w http.ResponseWriter, r *http.Request) {
	if s.configPath != "" {
		config.MarkRenameNoticeSeen(s.configPath)
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
