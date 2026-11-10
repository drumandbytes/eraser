//go:build linux

package config

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"strings"
)

// MachineIDFileEnv points at a file holding the machine ID, created on first
// use. The Docker image sets it to a path in the data volume: a container has
// no /etc/machine-id and a new hostname on every recreate, so without it the
// key for the encrypted mail passwords would change with the container.
const MachineIDFileEnv = "DELISTD_MACHINE_ID_FILE"

func readMachineID() (string, error) {
	if p := Getenv(MachineIDFileEnv); p != "" {
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
// is written under a unique temp name and hard-linked into place: os.Link
// never replaces an existing file, so when several containers start on a
// fresh volume at once the first ID wins and nobody reads a partial one.
func ensureMachineIDFile(path string) (string, error) {
	read := func() string {
		b, _ := os.ReadFile(path)
		return strings.TrimSpace(string(b))
	}
	if id := read(); id != "" {
		return id, nil
	}
	raw := make([]byte, 16)
	_, _ = rand.Read(raw)
	id := hex.EncodeToString(raw)
	tmp := path + "." + id // unique per process, so PID 1 in every container is fine
	if err := os.WriteFile(tmp, []byte(id+"\n"), 0o600); err != nil {
		return "", err
	}
	defer func() { _ = os.Remove(tmp) }()
	_ = os.Link(tmp, path) // EEXIST: another process won, read its ID
	if won := read(); won != "" {
		return won, nil
	}
	return "", os.ErrNotExist
}
