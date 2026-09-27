package history

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"
)

// Two stores on one file stand in for `eraser serve` and a CLI/scheduled run
// writing at once, each with a few concurrent writers (send job, inbox scan).
// Without busy_timeout, one side gets SQLITE_BUSY.
func TestConcurrentStoresOnOneFile(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "history.db")
	var stores []*Store
	for range 2 {
		s, err := NewStore(dbPath)
		if err != nil {
			t.Fatalf("NewStore: %v", err)
		}
		t.Cleanup(func() { _ = s.Close() })
		stores = append(stores, s)
	}

	var wg sync.WaitGroup
	errs := make(chan error, 200)
	for i, s := range stores {
		for g := range 4 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := range 25 {
					id := fmt.Sprintf("b%d-%d-%d", i, g, j)
					errs <- s.Add(&Record{BrokerID: id, BrokerName: id, Email: id + "@example.com", Template: "gdpr", Status: StatusSent, SentAt: time.Now()})
				}
			}()
		}
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent Add: %v", err)
		}
	}

	n, err := stores[0].CountSentSince("", time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatalf("CountSentSince: %v", err)
	}
	if n != 200 {
		t.Fatalf("got %d rows, want 200", n)
	}
}

func TestStoreFilesArePrivate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no Unix permission bits on Windows")
	}
	dir := t.TempDir()
	s, err := NewStore(filepath.Join(dir, "history.db"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer func() { _ = s.Close() }()
	addRecord(t, s, "b", StatusSent, time.Now())

	for _, name := range []string{"history.db", "history.db-wal"} {
		fi, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("stat %s: %v", name, err)
		}
		if perm := fi.Mode().Perm(); perm&0o077 != 0 {
			t.Errorf("%s mode %o is readable by others", name, perm)
		}
	}
}

// A '?' or '#' in the config directory must not truncate the DSN and silently
// open a different database without the pragmas.
func TestStoreOddPathKeepsPragmas(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "we?ird#dir")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(dir, "history.db")
	s, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer func() { _ = s.Close() }()

	var mode string
	if err := s.db.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil || mode != "wal" {
		t.Fatalf("journal_mode = %q (%v), want wal", mode, err)
	}
	var seq int
	var name, file string
	if err := s.db.QueryRow("PRAGMA database_list").Scan(&seq, &name, &file); err != nil {
		t.Fatal(err)
	}
	if want, _ := filepath.EvalSymlinks(dbPath); file != want && file != dbPath {
		t.Fatalf("opened %q, want %q", file, dbPath)
	}
}
