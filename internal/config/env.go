package config

import (
	"os"
	"strings"
)

// Getenv reads a DELISTD_* variable, falling back to its pre-rename ERASER_*
// name so existing Docker/compose/systemd setups keep working. The fallback
// goes away in the next major version.
func Getenv(name string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	if legacy, ok := strings.CutPrefix(name, "DELISTD_"); ok {
		return os.Getenv("ERASER_" + legacy)
	}
	return ""
}
