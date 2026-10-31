package config

import "os"

// The "what's new in 2.0" notice is shown once per install, in the web UI and
// on the first CLI command. A marker file next to config.yaml remembers it.
func whatsNewMarker(configPath string) string { return configPath + ".whats-new-2" }

// WhatsNewSeen reports whether the 2.0 notice was already shown or dismissed.
func WhatsNewSeen(configPath string) bool {
	_, err := os.Stat(whatsNewMarker(configPath))
	return err == nil
}

// MarkWhatsNewSeen records that the notice was shown. Fresh installs call it
// when setup finishes, so only upgraders see it.
func MarkWhatsNewSeen(configPath string) {
	_ = os.WriteFile(whatsNewMarker(configPath), nil, 0o600)
}
