package web

import (
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/drumandbytes/eraser/internal/config"
)

// fileStamp identifies a config file version cheaply; "" when it can't be read.
func fileStamp(path string) string {
	st, err := os.Stat(path)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%d/%d", st.ModTime().UnixNano(), st.Size())
}

// refreshConfig reloads config.yaml when it changed on disk, so edits made
// with the CLI (`eraser init`, `profile edit`, ...) show up in a running
// `eraser serve` without a restart. A file that fails to load keeps the
// current config; the next change is tried again.
func (s *Server) refreshConfig() {
	if s.configPath == "" {
		return
	}
	stamp := fileStamp(s.configPath)
	if stamp == "" {
		return
	}
	s.reloadMu.Lock()
	defer s.reloadMu.Unlock()
	if s.cfgStamp == "" { // configPath was set after NewServer (tests): adopt the file as current
		s.cfgStamp = stamp
		return
	}
	if stamp == s.cfgStamp {
		return
	}
	s.cfgStamp = stamp
	cfg, err := config.Load(s.configPath)
	if err != nil {
		log.Printf("config reload: %v", err)
		return
	}
	s.config.Store(cfg)
	s.cfgStamp = fileStamp(s.configPath) // Load may have migrated and rewritten the file
}

// saveConfig writes cfg and remembers the resulting file version, so the web
// UI's own saves aren't mistaken for outside edits.
func (s *Server) saveConfig(cfg *config.Config) error {
	s.reloadMu.Lock()
	defer s.reloadMu.Unlock()
	if err := config.Save(s.configPath, cfg); err != nil {
		return err
	}
	s.cfgStamp = fileStamp(s.configPath)
	return nil
}

func (s *Server) reloadConfigOnChange(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.refreshConfig()
		next.ServeHTTP(w, r)
	})
}
