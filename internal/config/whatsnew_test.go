package config

import (
	"path/filepath"
	"testing"
)

func TestWhatsNewMarker(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.yaml")
	if WhatsNewSeen(p) {
		t.Fatal("not seen before it's marked")
	}
	MarkWhatsNewSeen(p)
	if !WhatsNewSeen(p) {
		t.Error("marker not remembered")
	}
}

func TestRenameNoticeMarker(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.yaml")
	if RenameNoticeSeen(p) {
		t.Fatal("not seen before it's marked")
	}
	MarkRenameNoticeSeen(p)
	if !RenameNoticeSeen(p) {
		t.Error("marker not remembered")
	}
	if WhatsNewSeen(p) {
		t.Error("the rename marker must not settle the 2.0 notice")
	}
}

func TestGetenvFallsBackToTheLegacyName(t *testing.T) {
	t.Setenv("DELISTD_X", "")
	t.Setenv("ERASER_X", "")
	if got := Getenv("DELISTD_X"); got != "" {
		t.Fatalf("unset = %q", got)
	}
	t.Setenv("ERASER_X", "old")
	if got := Getenv("DELISTD_X"); got != "old" {
		t.Errorf("legacy fallback = %q", got)
	}
	t.Setenv("DELISTD_X", "new")
	if got := Getenv("DELISTD_X"); got != "new" {
		t.Errorf("new name must win, got %q", got)
	}
	if got := Getenv("OTHER_Y"); got != "" {
		t.Errorf("a name without the DELISTD_ prefix has no legacy twin, got %q", got)
	}
}
