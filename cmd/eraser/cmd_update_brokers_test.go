package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/drumandbytes/eraser/internal/broker"
)

func TestRunUpdateBrokers(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// need >= broker.MinSaneBrokerCount entries to pass the guard
	yaml := "brokers:\n"
	for i := 0; i < broker.MinSaneBrokerCount+5; i++ {
		yaml += "  - {id: b" + itoa(i) + ", name: B" + itoa(i) + ", email: b@x.y, region: us}\n"
	}

	var served int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"v1"`)
		if r.Header.Get("If-None-Match") == `"v1"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		served++
		_, _ = w.Write([]byte(yaml))
	}))
	defer srv.Close()

	// First run downloads.
	if err := runUpdateBrokers(srv.URL, false); err != nil {
		t.Fatalf("first run: %v", err)
	}
	if served != 1 {
		t.Fatalf("expected one download, got %d", served)
	}
	target := filepath.Join(home, ".eraser", "brokers.yaml")
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("brokers.yaml not written: %v", err)
	}
	if etag, _ := os.ReadFile(filepath.Join(home, ".eraser", "brokers.etag")); string(etag) != `"v1"` {
		t.Errorf("etag = %q", etag)
	}

	// Second run: conditional request -> 304, no re-download.
	if err := runUpdateBrokers(srv.URL, false); err != nil {
		t.Fatalf("second run: %v", err)
	}
	if served != 1 {
		t.Errorf("304 path re-downloaded: served=%d", served)
	}
}

func TestRunUpdateBrokersRejectsTinyList(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("brokers:\n  - {id: only, name: Only, email: a@b.c, region: us}\n"))
	}))
	defer srv.Close()

	if err := runUpdateBrokers(srv.URL, false); err == nil {
		t.Fatal("expected refusal for a too-small list")
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}
