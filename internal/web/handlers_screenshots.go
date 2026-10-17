package web

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"github.com/go-chi/chi/v5"
)

// Screenshots can hold personal data: resolve them through a task the active
// profile owns, never a public dir or a supplied filename.
func (s *Server) handleTaskScreenshot(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	id, err := strconv.ParseInt(chi.URLParam(r, "taskID"), 10, 64)
	if err != nil || s.historyStore == nil {
		http.NotFound(w, r)
		return
	}
	task, err := s.historyStore.GetPendingTaskByID(id, s.activeProfile(r).ID)
	if err != nil || task == nil || task.ScreenshotPath == "" || filepath.Base(task.ScreenshotPath) != task.ScreenshotPath {
		http.NotFound(w, r)
		return
	}
	f, err := os.OpenInRoot(filepath.Join(s.dataDir, "screenshots"), task.ScreenshotPath)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		http.NotFound(w, r)
		return
	}
	var header [512]byte
	n, _ := f.Read(header[:])
	kind := http.DetectContentType(header[:n])
	if kind != "image/png" && kind != "image/jpeg" {
		http.NotFound(w, r)
		return
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", kind)
	http.ServeContent(w, r, task.ScreenshotPath, info.ModTime(), f)
}
