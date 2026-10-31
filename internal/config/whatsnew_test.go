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
