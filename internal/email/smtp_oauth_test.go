package email

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/drumandbytes/delistd/internal/config"
	"github.com/drumandbytes/delistd/internal/oauth"
)

func fakeTokenServer(t *testing.T, status int, body map[string]any) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(body)
	}))
	old := oauth.MicrosoftConsumers
	oauth.MicrosoftConsumers = oauth.Endpoints{Token: srv.URL, Scopes: old.Scopes}
	t.Cleanup(func() { srv.Close(); oauth.MicrosoftConsumers = old })
}

// xoauth2SMTPServer accepts STARTTLS then AUTH XOAUTH2 and reports the AUTH line.
func xoauth2SMTPServer(t *testing.T, cert tls.Certificate) (addr string, authLine <-chan string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	out := make(chan string, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		r := bufio.NewReader(conn)
		reply := func(s string) { _, _ = conn.Write([]byte(s + "\r\n")) }
		inTLS := false
		reply("220 test")
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			cmd := strings.TrimSpace(line)
			switch up := strings.ToUpper(cmd); {
			case strings.HasPrefix(up, "EHLO") && !inTLS:
				reply("250-test\r\n250 STARTTLS")
			case strings.HasPrefix(up, "EHLO"):
				reply("250-test\r\n250 AUTH XOAUTH2")
			case strings.HasPrefix(up, "STARTTLS"):
				reply("220 go ahead")
				conn = tls.Server(conn, &tls.Config{Certificates: []tls.Certificate{cert}})
				r = bufio.NewReader(conn)
				inTLS = true
			case strings.HasPrefix(up, "AUTH"):
				out <- cmd
				reply("235 ok")
			case strings.HasPrefix(up, "DATA"):
				reply("354 go")
				for {
					l, err := r.ReadString('\n')
					if err != nil || l == ".\r\n" {
						break
					}
				}
				reply("250 ok")
			case strings.HasPrefix(up, "QUIT"):
				reply("221 bye")
				return
			default:
				reply("250 ok")
			}
		}
	}()
	return ln.Addr().String(), out
}

func TestSendWithOAuth2(t *testing.T) {
	fakeTokenServer(t, 200, map[string]any{"access_token": "ACCESS", "expires_in": 3600})
	ts := httptest.NewTLSServer(http.NotFoundHandler())
	t.Cleanup(ts.Close)
	addr, authLine := xoauth2SMTPServer(t, ts.TLS.Certificates[0])
	host, port := splitHostPortForTest(t, addr)
	s := NewSMTPSender(config.SMTPConfig{Host: host, Port: port, Username: "jane@outlook.com", Password: "refresh-1", Auth: config.AuthOAuth2, ClientID: "cid"}, "jane@outlook.com")

	res := s.Send(context.Background(), Message{To: "privacy@acme.example", From: "jane@outlook.com", Subject: "Erasure request", Body: "hi"})
	if !res.Success {
		t.Fatalf("send failed: %v", res.Error)
	}
	fields := strings.Fields(<-authLine)
	if len(fields) != 3 || fields[1] != "XOAUTH2" {
		t.Fatalf("AUTH line = %v", fields)
	}
	raw, _ := base64.StdEncoding.DecodeString(fields[2])
	if string(raw) != "user=jane@outlook.com\x01auth=Bearer ACCESS\x01\x01" {
		t.Fatalf("initial response = %q", raw)
	}
}

func TestSendWithOAuth2NeedsFreshSignIn(t *testing.T) {
	fakeTokenServer(t, 400, map[string]any{"error": "invalid_grant"})
	s := NewSMTPSender(config.SMTPConfig{Host: "127.0.0.1", Port: 587, Username: "jane@outlook.com", Password: "revoked", Auth: config.AuthOAuth2, ClientID: "cid-revoked"}, "jane@outlook.com")
	res := s.Send(context.Background(), Message{To: "privacy@acme.example", From: "jane@outlook.com", Subject: "x", Body: "y"})
	if res.Success || res.Error == nil || !strings.Contains(res.Error.Error(), "oauth login") {
		t.Fatalf("got %+v", res)
	}
}
