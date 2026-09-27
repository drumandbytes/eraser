package schedule

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// State is the outcome of the last cycle, written by every mode so
// `eraser schedule status` and the web UI can show it.
type State struct {
	LastRun time.Time `json:"last_run"`
	Mode    string    `json:"mode"` // "os", "loop", "once", "serve"
	Sent    int       `json:"sent"`
	Error   string    `json:"error,omitempty"`
}

func statePath(dir string) string { return filepath.Join(dir, "auto-state.json") }

func SaveState(dir string, s State) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := statePath(dir) + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return fmt.Errorf("failed to write schedule state: %w", err)
	}
	return os.Rename(tmp, statePath(dir))
}

// LoadState returns nil, nil when no cycle has run yet.
func LoadState(dir string) (*State, error) {
	data, err := os.ReadFile(statePath(dir))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read schedule state: %w", err)
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("failed to parse schedule state: %w", err)
	}
	return &s, nil
}
