package web

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/drumandbytes/eraser/internal/history"
)

func TestTaskScreenshotOwnershipAndFileBoundary(t *testing.T) {
	s := smokeServer(t)
	s.dataDir = t.TempDir()
	dir := filepath.Join(s.dataDir, "screenshots")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	var img bytes.Buffer
	if err := png.Encode(&img, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "receipt.png"), img.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(s.dataDir, "private.png")
	if err := os.WriteFile(outside, img.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "link.png")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "not-an-image.png"), []byte("private configuration"), 0600); err != nil {
		t.Fatal(err)
	}
	router := s.setupRouter()
	for _, tc := range []struct {
		name, file, profile string
		want                int
	}{
		{"owner", "receipt.png", "default", http.StatusOK},
		{"other profile", "receipt.png", "spouse", http.StatusNotFound},
		{"traversal", "../private.png", "default", http.StatusNotFound},
		{"symlink", "link.png", "default", http.StatusNotFound},
		{"non-image", "not-an-image.png", "default", http.StatusNotFound},
		{"missing", "missing.png", "default", http.StatusNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			task := &history.PendingTask{ProfileID: "default", BrokerID: "spokeo", TaskType: history.TaskManualForm, ScreenshotPath: tc.file}
			if err := s.historyStore.AddPendingTask(task); err != nil {
				t.Fatal(err)
			}
			r := loopbackRequest(http.MethodGet, fmt.Sprintf("/tasks/%d/screenshot", task.ID), nil)
			r.AddCookie(&http.Cookie{Name: activeProfileCookie, Value: tc.profile})
			w := httptest.NewRecorder()
			router.ServeHTTP(w, r)
			if w.Code != tc.want {
				t.Fatalf("status = %d, want %d", w.Code, tc.want)
			}
			if tc.want == http.StatusOK && (!bytes.Equal(w.Body.Bytes(), img.Bytes()) || w.Header().Get("Content-Type") != "image/png" || w.Header().Get("Cache-Control") != "private, no-store") {
				t.Fatal("screenshot bytes or privacy headers missing")
			}
		})
	}
}

func TestAutomationReviewStatsDoNotDoubleCountTasks(t *testing.T) {
	s := smokeServer(t)
	for _, tc := range []struct{ profile, note string }{
		{"default", "MCP: needs_user. Human verification"},
		{"default", "MCP: unknown. Rejected form"},
		{"default", "MCP: browser open; automation in progress."},
		{"spouse", "MCP: unknown. Another profile"},
	} {
		if err := s.historyStore.AddPendingTask(&history.PendingTask{ProfileID: tc.profile, BrokerID: "spokeo", TaskType: history.TaskManualForm, Notes: tc.note}); err != nil {
			t.Fatal(err)
		}
	}
	got := s.getPipelineStats("default")
	if got.PendingTasks != 3 || got.NeedsReview != 2 || got.Confirmed != 0 {
		t.Fatalf("stats = %+v", got)
	}
}
