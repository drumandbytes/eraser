//go:build linux

package config

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestMachineIDFileConcurrentFirstStart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "machine-id")
	ids := make([]string, 8)
	var wg sync.WaitGroup
	for i := range ids {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ids[i], _ = ensureMachineIDFile(path)
		}()
	}
	wg.Wait()
	for _, id := range ids {
		if id == "" || id != ids[0] {
			t.Fatalf("ids disagree: %q", ids)
		}
	}
	if got, _ := ensureMachineIDFile(path); got != ids[0] {
		t.Errorf("second call = %q, want %q", got, ids[0])
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600", fi.Mode().Perm())
	}
	if m, _ := filepath.Glob(path + ".*"); len(m) != 0 {
		t.Errorf("temp files left: %v", m)
	}
}

func TestReadMachineIDUsesFileEnv(t *testing.T) {
	path := filepath.Join(t.TempDir(), "machine-id")
	t.Setenv(MachineIDFileEnv, path)
	id, err := readMachineID()
	if err != nil || id == "" {
		t.Fatalf("readMachineID() = %q, %v", id, err)
	}
	if again, _ := readMachineID(); again != id {
		t.Errorf("second read = %q, want %q", again, id)
	}
}

func TestEnsureMachineIDFileErrors(t *testing.T) {
	if _, err := ensureMachineIDFile(filepath.Join(t.TempDir(), "missing-dir", "machine-id")); err == nil {
		t.Error("unwritable directory: want an error")
	}
	// a directory in the way: the link can't win and there's no ID to read
	if _, err := ensureMachineIDFile(t.TempDir()); err == nil {
		t.Error("path is a directory: want an error")
	}
}
