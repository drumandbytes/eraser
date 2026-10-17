package browser

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The file name must say what is inside: Chrome returns JPEG below quality 100
// and PNG at 100, and screenshots used to be JPEG data named .png.
func TestScreenshotFilesAreNamedByTheirFormat(t *testing.T) {
	b := requireChrome(t)
	b.config.ScreenshotDir = t.TempDir()

	for _, tc := range []struct {
		quality int
		ext     string
		magic   []byte
	}{
		{90, ".jpg", []byte{0xFF, 0xD8, 0xFF}},
		{100, ".png", []byte("\x89PNG")},
	} {
		name, err := b.takeScreenshotQuality(b.ctx, "acme", "shot", tc.quality)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasSuffix(name, tc.ext) {
			t.Errorf("quality %d: file %q, want a %s extension", tc.quality, name, tc.ext)
		}
		data, err := os.ReadFile(filepath.Join(b.config.ScreenshotDir, name))
		if err != nil || !bytes.HasPrefix(data, tc.magic) {
			t.Errorf("quality %d: %q does not start with the %s signature (%v)", tc.quality, name, tc.ext, err)
		}
	}
}
