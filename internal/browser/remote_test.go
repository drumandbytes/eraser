package browser

import (
	"strings"
	"testing"

	"github.com/drumandbytes/eraser/internal/config"
)

// An unreachable endpoint: nothing here needs a real Chrome.
const deadEndpoint = "http://127.0.0.1:1"

func TestNewReadsRemoteURLFromEnv(t *testing.T) {
	t.Setenv(config.ChromeURLEnv, deadEndpoint)
	b, err := New(DefaultConfig(), &config.Profile{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if b.config.RemoteURL != deadEndpoint {
		t.Errorf("RemoteURL = %q, want the env value", b.config.RemoteURL)
	}
}

func TestExplicitChromePathBeatsEnv(t *testing.T) {
	t.Setenv(config.ChromeURLEnv, deadEndpoint)
	cfg := DefaultConfig()
	cfg.ExecutablePath = "/nonexistent/chrome"
	b, err := New(cfg, &config.Profile{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if b.config.RemoteURL != "" {
		t.Errorf("RemoteURL = %q, want empty when ExecutablePath is set", b.config.RemoteURL)
	}
}

func TestStartRemoteUnreachable(t *testing.T) {
	cfg := DefaultConfig()
	cfg.RemoteURL = deadEndpoint
	b, err := New(cfg, &config.Profile{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if err := b.start(); err == nil {
		t.Error("start() against a dead endpoint: want an error")
	}
}

func TestStartInContainerWithoutBrowserSaysWhatToSet(t *testing.T) {
	t.Setenv(config.ContainerEnv, "1")
	t.Setenv(config.ChromeURLEnv, "")
	cfg := DefaultConfig()
	cfg.ExecutablePath = "/nonexistent/chrome"
	b, err := New(cfg, &config.Profile{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	err = b.start()
	if err == nil || !strings.Contains(err.Error(), config.ChromeURLEnv) {
		t.Errorf("start() error = %v, want a hint naming %s", err, config.ChromeURLEnv)
	}
}
