package web

import (
	"context"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func stubOpenBrowser(t *testing.T) func() []string {
	t.Helper()
	var mu sync.Mutex
	var opened []string
	orig := OpenBrowser
	t.Cleanup(func() { OpenBrowser = orig })
	OpenBrowser = func(url string) { mu.Lock(); opened = append(opened, url); mu.Unlock() }
	return func() []string { mu.Lock(); defer mu.Unlock(); return append([]string(nil), opened...) }
}

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	return ln.Addr().(*net.TCPAddr).Port
}

func TestStartServesAndShutsDown(t *testing.T) {
	opened := stubOpenBrowser(t)
	s := smokeServer(t)
	s.port = freePort(t)
	done := make(chan error, 1)
	go func() { done <- s.Start() }()

	url := "http://127.0.0.1:" + strconv.Itoa(s.port) + "/"
	deadline := time.Now().Add(10 * time.Second)
	for {
		resp, err := http.Get(url)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("GET / = %d", resp.StatusCode)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("server never came up: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	for len(opened()) == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if got := opened(); len(got) != 1 || !strings.HasSuffix(got[0], ":"+strconv.Itoa(s.port)) {
		t.Errorf("browser opened %v", got)
	}

	if err := s.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Errorf("Start after Shutdown = %v", err)
	}
}

// Ctrl+C can land before Start has built the server: no panic, and Start
// then returns instead of serving.
func TestShutdownBeforeStart(t *testing.T) {
	stubOpenBrowser(t)
	s := smokeServer(t)
	if err := s.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := s.Start(); err != nil {
		t.Errorf("Start after Shutdown = %v", err)
	}
}

func TestStartPortInUse(t *testing.T) {
	stubOpenBrowser(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	s := smokeServer(t)
	s.port = ln.Addr().(*net.TCPAddr).Port
	if err := s.Start(); err == nil || !strings.Contains(err.Error(), "server error") {
		t.Errorf("Start on a taken port = %v", err)
	}
}

func TestBrowserCommand(t *testing.T) {
	for goos, want := range map[string]string{"darwin": "open", "linux": "xdg-open", "windows": "cmd", "plan9": ""} {
		if cmd, _ := browserCommand(goos, "http://localhost:8080"); cmd != want {
			t.Errorf("%s: %q, want %q", goos, cmd, want)
		}
	}
}
