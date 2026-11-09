//go:build linux

package config

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
)

// MachineIDFileEnv points at a file holding the machine ID, created on first
// use. The Docker image sets it to a path in the data volume: a container has
// no /etc/machine-id and a new hostname on every recreate, so without it the
// key for the encrypted mail passwords would change with the container.
const MachineIDFileEnv = "ERASER_MACHINE_ID_FILE"

func readMachineID() (string, error) {
	if p := os.Getenv(MachineIDFileEnv); p != "" {
		return ensureMachineIDFile(p)
	}
	for _, p := range []string{"/etc/machine-id", "/var/lib/dbus/machine-id"} {
		if b, err := os.ReadFile(p); err == nil && strings.TrimSpace(string(b)) != "" {
			return strings.TrimSpace(string(b)), nil
		}
	}
	return "", os.ErrNotExist
}

// ensureMachineIDFile returns the ID in path, creating it if missing. The ID
// is written under a temp name and hard-linked into place: os.Link never
// replaces an existing file, so when several containers start on a fresh
// volume at once the first ID wins and nobody reads a partial one.
func ensureMachineIDFile(path string) (string, error) {
	read := func() string {
		b, _ := os.ReadFile(path)
		return strings.TrimSpace(string(b))
	}
	if id := read(); id != "" {
		return id, nil
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".machine-id-*")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	raw := make([]byte, 16)
	_, _ = rand.Read(raw)
	if _, err := tmp.WriteString(hex.EncodeToString(raw) + "\n"); err != nil {
		_ = tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	_ = os.Link(tmp.Name(), path) // EEXIST: another process won, read its ID
	if id := read(); id != "" {
		return id, nil
	}
	return "", os.ErrNotExist
}
