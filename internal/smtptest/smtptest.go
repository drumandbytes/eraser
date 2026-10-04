// Package smtptest is a plaintext SMTP relay for send tests.
package smtptest

import (
	"bufio"
	"net"
	"strings"
	"sync"
	"testing"

	"github.com/drumandbytes/eraser/internal/config"
)

type Relay struct {
	Host string
	Port int

	mu       sync.Mutex
	rejected map[string]bool
	authFail bool
	got      []string
}

// Start accepts any number of connections until the test ends.
func Start(t testing.TB) *Relay {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	r := &Relay{Host: "127.0.0.1", Port: ln.Addr().(*net.TCPAddr).Port, rejected: map[string]bool{}}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go r.serve(conn)
		}
	}()
	return r
}

// Reject makes RCPT TO addr answer 550 (or accept again with false).
func (r *Relay) Reject(addr string, reject bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rejected[addr] = reject
}

// FailAuth answers MAIL FROM with 535, like a provider that locked the account.
func (r *Relay) FailAuth() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.authFail = true
}

// Recipients lists every accepted RCPT TO, in order.
func (r *Relay) Recipients() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.got...)
}

// Email is a sending config for this relay (no TLS, no auth).
func (r *Relay) Email(from string) config.EmailConfig {
	noTLS := false
	return config.EmailConfig{From: from, SMTP: config.SMTPConfig{Host: r.Host, Port: r.Port, UseTLS: &noTLS}}
}

func (r *Relay) serve(conn net.Conn) {
	defer func() { _ = conn.Close() }()
	br := bufio.NewReader(conn)
	reply := func(s string) { _, _ = conn.Write([]byte(s + "\r\n")) }
	reply("220 test")
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return
		}
		cmd := strings.TrimSpace(line)
		up := strings.ToUpper(cmd)
		r.mu.Lock()
		authFail := r.authFail
		r.mu.Unlock()
		switch {
		case strings.HasPrefix(up, "MAIL") && authFail:
			reply("535 5.7.8 authentication failed")
		case strings.HasPrefix(up, "RCPT"):
			to := strings.Trim(cmd[strings.Index(cmd, ":")+1:], "<> ")
			r.mu.Lock()
			rejected := r.rejected[to]
			if !rejected {
				r.got = append(r.got, to)
			}
			r.mu.Unlock()
			if rejected {
				reply("550 no such user")
			} else {
				reply("250 ok")
			}
		case strings.HasPrefix(up, "DATA"):
			reply("354 go ahead")
			for {
				l, err := br.ReadString('\n')
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
