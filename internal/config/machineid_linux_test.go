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
	if m, _ := filepath.Glob(filepath.Join(filepath.Dir(path), ".machine-id-*")); len(m) != 0 {
		t.Errorf("temp files left: %v", m)
	}
}
