package web

import (
	"context"
	"net/http"
	"strconv"
	"testing"
	"time"
)

func TestListenAddr(t *testing.T) {
	for _, tc := range []struct {
		host string
		port int
		want string
	}{
		{"", 8080, "127.0.0.1:8080"},
		{"127.0.0.1", 3000, "127.0.0.1:3000"},
		{"0.0.0.0", 8080, "0.0.0.0:8080"},
		{"::1", 8080, "[::1]:8080"},
	} {
		if got := listenAddr(tc.host, tc.port); got != tc.want {
			t.Errorf("listenAddr(%q, %d) = %q, want %q", tc.host, tc.port, got, tc.want)
		}
	}
}

func TestIsLoopbackListen(t *testing.T) {
	for host, want := range map[string]bool{
		"":          true,
		"127.0.0.1": true,
		"127.0.0.2": true,
		"::1":       true,
		"localhost": true,
		"LocalHost": true,
		"0.0.0.0":   false,
		"::":        false,
		"10.0.0.5":  false,
		"eraser":    false,
	} {
		if got := IsLoopbackListen(host); got != want {
			t.Errorf("IsLoopbackListen(%q) = %v, want %v", host, got, want)
		}
	}
}

// serve --listen 0.0.0.0 (the Docker image): the server answers on every
// interface, keeps the loopback Host check, and doesn't try to open a browser.
func TestStartOnAllInterfaces(t *testing.T) {
	opened := stubOpenBrowser(t)
	s := smokeServer(t)
	s.port = freePort(t)
	s.ListenHost = "0.0.0.0"
	done := make(chan error, 1)
	go func() { done <- s.Start() }()

	base := "http://127.0.0.1:" + strconv.Itoa(s.port) + "/"
	deadline := time.Now().Add(10 * time.Second)
	for {
		resp, err := http.Get(base)
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

	// a non-loopback Host is still refused, whatever the bind address
	req, _ := http.NewRequest(http.MethodGet, base, nil)
	req.Host = "192.168.1.20:" + strconv.Itoa(s.port)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("GET with LAN Host = %d, want 403", resp.StatusCode)
	}

	if err := s.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Errorf("Start after Shutdown = %v", err)
	}
	if got := opened(); len(got) != 0 {
		t.Errorf("browser opened %v on a non-loopback bind", got)
	}
}
