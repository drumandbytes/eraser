package web

import (
	"archive/zip"
	"fmt"
	"net/http"
	"strings"

	"github.com/drumandbytes/eraser/internal/broker"
	emaTemplate "github.com/drumandbytes/eraser/internal/template"
)

// handleDraftsZip is `eraser draft -o dir` as a download: one .eml per broker
// with an email address that is in scope for the active profile, to send by
// hand and then record with Mark sent.
func (s *Server) handleDraftsZip(w http.ResponseWriter, r *http.Request) {
	cfg := s.getConfig()
	if cfg == nil {
		http.Error(w, "Finish setup first", http.StatusBadRequest)
		return
	}
	active := s.activeProfile(r)
	from := cfg.EmailForProfile(active).From
	if from == "" {
		from = active.Email
	}

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="eraser-drafts.zip"`)
	zw := zip.NewWriter(w)
	defer func() { _ = zw.Close() }()

	brokers := broker.ForCountry(s.brokers().Filter(cfg.Options.Regions, cfg.Options.ExcludedBrokers, cfg.Options.ExcludedCategories), active.Country)
	for _, b := range brokers {
		if strings.TrimSpace(b.Email) == "" {
			continue
		}
		email, err := s.tmplEngine.Render(s.configuredTemplate(), active.Profile, b)
		if err != nil {
			continue
		}
		f, err := zw.Create(b.ID + ".eml")
		if err != nil {
			return
		}
		_, _ = fmt.Fprint(f, string(emaTemplate.FormatEML(from, b.Email, email)))
	}
}
