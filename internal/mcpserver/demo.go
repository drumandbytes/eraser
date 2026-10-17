package mcpserver

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/drumandbytes/eraser/internal/broker"
	"github.com/drumandbytes/eraser/internal/config"
	"github.com/drumandbytes/eraser/internal/history"
)

type demoState struct{ origin string }

// NewDemo builds an isolated config and fake broker site. The loopback exception
// is tied to this listener, never a tool argument.
func NewDemo(headless bool, chromePath string) (*Service, func(), error) {
	dir, err := os.MkdirTemp("", "eraser-mcp-demo-")
	if err != nil {
		return nil, nil, err
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, nil, err
	}
	origin := "http://" + listener.Addr().String()
	srv := &http.Server{Handler: demoHandler(), ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = srv.Serve(listener) }()
	cleanup := func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}
	bs := []broker.Broker{}
	for _, item := range []struct{ ID, Name, Path string }{
		{"demo-form", "Demo: simple form", "/form"},
		{"demo-steps", "Demo: multiple steps", "/steps"},
		{"demo-verification", "Demo: human handoff", "/verification"},
		{"demo-confirm", "Demo: confirmation link", "/confirm"},
		{"demo-error", "Demo: rejected submission", "/error-form"},
	} {
		bs = append(bs, broker.Broker{ID: item.ID, Name: item.Name, Website: origin, OptOutURL: origin + item.Path, Region: "eu"})
	}
	db := &broker.BrokerDatabase{Brokers: bs}
	brokerPath := filepath.Join(dir, "brokers.yaml")
	if err := db.Save(brokerPath); err != nil {
		cleanup()
		return nil, nil, err
	}
	cfg := &config.Config{Profiles: []config.NamedProfile{{ID: "default", Profile: config.Profile{FirstName: "Demo", LastName: "Person", Email: "demo@example.invalid", Country: "Norway", City: "Oslo"}}}, Options: config.Options{SendMode: "manual", Template: "gdpr", BrokerFile: brokerPath, Regions: []string{"eu"}, DailySendLimit: 10}}
	configPath := filepath.Join(dir, "config.yaml")
	if err := config.Save(configPath, cfg); err != nil {
		cleanup()
		return nil, nil, err
	}
	s, err := New(Options{ConfigPath: configPath, BrokerPath: brokerPath, Headless: headless, ChromePath: chromePath})
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	s.demo = &demoState{origin: origin}
	for _, b := range bs {
		kind := history.TaskManualForm
		if b.ID == "demo-confirm" {
			kind = history.TaskConfirm
		}
		if _, err := s.store.EnsureAutomationTask("default", b.ID, b.Name, kind, b.OptOutURL, "human"); err != nil {
			_ = s.Close()
			cleanup()
			return nil, nil, err
		}
	}
	fmt.Fprintf(os.Stderr, "Isolated demo data: %s\nReview with: eraser --config %s serve\n", dir, configPath)
	return s, cleanup, nil
}

func demoHandler() http.Handler {
	mux := http.NewServeMux()
	write := func(w http.ResponseWriter, body string) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprint(w, `<!doctype html><html><head><title>Eraser local demo</title></head><body><h1>Fictional data broker</h1><p>This is a local test. No request reaches a real broker.</p>`+body+`</body></html>`)
	}
	mux.HandleFunc("/form", func(w http.ResponseWriter, r *http.Request) {
		write(w, `<h2>Remove my information</h2><form method="post" action="/received"><label>First name <input name="first_name" required></label><label>Email <input type="email" name="email" required></label><label>Country <select name="country"><option>Norway</option><option>Sweden</option></select></label><label><input type="checkbox" name="consent" required>I request deletion of my personal data</label><button type="submit">Submit request</button></form>`)
	})
	mux.HandleFunc("/steps", func(w http.ResponseWriter, r *http.Request) {
		write(w, `<h2>Step one</h2><form onsubmit="event.preventDefault();document.querySelector('h2').textContent='Step two';this.onsubmit=null;this.method='post';this.action='/received';this.innerHTML='<label>City <input name=city required></label><button type=submit>Submit request</button>'"><label>Email <input type="email" required></label><button type="submit">Next step</button></form>`)
	})
	mux.HandleFunc("/verification", func(w http.ResponseWriter, r *http.Request) {
		write(w, `<section id="challenge"><h2>Simulated human verification</h2><p>For this demo, click the button to complete the simulated challenge.</p><input name="captcha" placeholder="Security code" readonly><button onclick="document.getElementById('challenge').remove();document.getElementById('request').hidden=false">Complete demo verification</button></section><form id="request" hidden method="post" action="/received"><label>Email <input type="email" required></label><button type="submit">Submit request</button></form>`)
	})
	mux.HandleFunc("/confirm", func(w http.ResponseWriter, r *http.Request) {
		write(w, `<h2>Your request has been confirmed.</h2><p>We will process your removal request.</p>`)
	})
	mux.HandleFunc("/received", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "POST required", http.StatusMethodNotAllowed)
			return
		}
		write(w, `<h2>Your request has been received.</h2><p>Reference: DEMO-123. We will process your request.</p>`)
	})
	mux.HandleFunc("/error-form", func(w http.ResponseWriter, r *http.Request) {
		write(w, `<form method="post" action="/rejected"><label>Email <input name="email" type="email" required></label><button>Submit request</button></form>`)
	})
	mux.HandleFunc("/rejected", func(w http.ResponseWriter, r *http.Request) {
		write(w, `<h2>Your request was not submitted.</h2><p>Please contact support. This demonstrates an uncertain/failed result.</p>`)
	})
	return mux
}
