package main

import (
	"bufio"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// cliEnv is an isolated install: a temp HOME, a config file and a small
// broker list, so commands run end to end without touching the real machine.
type cliEnv struct {
	home, cfgPath, brokersPath string
}

const testBrokersYAML = `brokers:
  - id: acme
    name: Acme Data
    email: privacy@acme.example
    website: https://acme.example
    region: eu
    category: marketing
  - id: globex
    name: Globex
    email: dpo@globex.example
    opt_out_url: https://globex.example/optout
    region: us
    category: people-search
  - id: noemail
    name: No Email Co
    region: us
    category: people-search
`

// newCLIEnv writes cfgYAML (if non-empty) and the test broker list.
func newCLIEnv(t *testing.T, cfgYAML string) *cliEnv {
	t.Helper()
	dir := t.TempDir()
	e := &cliEnv{
		home:        filepath.Join(dir, "home"),
		cfgPath:     filepath.Join(dir, "config.yaml"),
		brokersPath: filepath.Join(dir, "brokers.yaml"),
	}
	if err := os.MkdirAll(e.home, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", e.home)
	t.Setenv("XDG_CONFIG_HOME", "")
	if cfgYAML != "" {
		if err := os.WriteFile(e.cfgPath, []byte(cfgYAML), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(e.brokersPath, []byte(testBrokersYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	return e
}

// run executes `eraser --config <cfg> --brokers <list> args...` with stdin
// fed from the given text and returns what it printed.
func (e *cliEnv) run(t *testing.T, stdin string, args ...string) (string, error) {
	t.Helper()
	return runCLI(t, stdin, append([]string{"--config", e.cfgPath, "--brokers", e.brokersPath}, args...)...)
}

// runCLI runs the real command tree in-process, swapping os.Stdin/Stdout.
func runCLI(t *testing.T, stdin string, args ...string) (string, error) {
	t.Helper()
	in, err := os.CreateTemp(t.TempDir(), "stdin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := in.WriteString(stdin); err != nil {
		t.Fatal(err)
	}
	if _, err := in.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	origIn, origOut := os.Stdin, os.Stdout
	os.Stdin, os.Stdout = in, w
	var out strings.Builder
	done := make(chan struct{})
	go func() { _, _ = io.Copy(&out, r); close(done) }()

	root := newRootCmd()
	root.SetArgs(args)
	root.SetOut(w)
	root.SetErr(io.Discard)
	root.SilenceUsage, root.SilenceErrors = true, true
	runErr := root.Execute()

	os.Stdin, os.Stdout = origIn, origOut
	_ = w.Close()
	<-done
	_ = in.Close()
	return out.String(), runErr
}

func mustContain(t *testing.T, out string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(out, w) {
			t.Errorf("output missing %q:\n%s", w, out)
		}
	}
}

// relay is a plaintext SMTP server for send tests; recipients in reject get
// a 550.
type relay struct {
	host     string
	port     int
	mu       sync.Mutex
	got      []string
	reject   map[string]bool
	authFail bool
}

func newRelay(t *testing.T) *relay {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	host, portStr, _ := net.SplitHostPort(ln.Addr().String())
	port, _ := strconv.Atoi(portStr)
	rl := &relay{host: host, port: port, reject: map[string]bool{}}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go rl.serve(conn)
		}
	}()
	return rl
}

func (rl *relay) serve(conn net.Conn) {
	defer func() { _ = conn.Close() }()
	r := bufio.NewReader(conn)
	reply := func(s string) { _, _ = conn.Write([]byte(s + "\r\n")) }
	reply("220 test")
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		cmd := strings.TrimSpace(line)
		rl.mu.Lock()
		authFail := rl.authFail
		rl.mu.Unlock()
		switch up := strings.ToUpper(cmd); {
		case strings.HasPrefix(up, "MAIL") && authFail:
			reply("535 5.7.8 authentication failed")
		case strings.HasPrefix(up, "RCPT"):
			to := strings.Trim(cmd[strings.Index(cmd, ":")+1:], "<> ")
			rl.mu.Lock()
			rejected := rl.reject[to]
			if !rejected {
				rl.got = append(rl.got, to)
			}
			rl.mu.Unlock()
			if rejected {
				reply("550 no such user")
			} else {
				reply("250 ok")
			}
		case strings.HasPrefix(up, "DATA"):
			reply("354 go ahead")
			for {
				l, err := r.ReadString('\n')
				if err != nil || l == ".\r\n" {
					break
				}
			}
			reply("250 queued")
		case strings.HasPrefix(up, "QUIT"):
			reply("221 bye")
			return
		default:
			reply("250 ok")
		}
	}
}

func (rl *relay) recipients() []string {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	return append([]string(nil), rl.got...)
}

// relayConfig is a valid single-profile config sending through rl.
func relayConfig(rl *relay, extra string) string {
	return `profiles:
  - id: default
    first_name: Jane
    last_name: Doe
    email: jane@example.com
    country: Latvia
email:
  from: jane@example.com
  smtp:
    host: ` + rl.host + `
    port: ` + strconv.Itoa(rl.port) + `
    use_tls: false
options:
  template: gdpr
  rate_limit_ms: 1
` + extra
}

// manualConfig is a manual-mode config with two profiles.
const manualConfig = `profiles:
  - id: default
    first_name: Jane
    last_name: Doe
    email: jane@example.com
  - id: spouse
    first_name: John
    last_name: Doe
    email: john@example.com
options:
  template: gdpr
  send_mode: manual
`
