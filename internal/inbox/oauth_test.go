package inbox

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/drumandbytes/delistd/internal/config"
	"github.com/drumandbytes/delistd/internal/oauth"
)

func fakeToken(t *testing.T, status int, body map[string]any) {
	t.Helper()
	tok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(body)
	}))
	old := oauth.MicrosoftConsumers
	oauth.MicrosoftConsumers = oauth.Endpoints{Token: tok.URL, Scopes: old.Scopes}
	t.Cleanup(func() { tok.Close(); oauth.MicrosoftConsumers = old })
}

func TestConnectWithOAuth2(t *testing.T) {
	fakeToken(t, 200, map[string]any{"access_token": "ACCESS", "expires_in": 3600})
	m, got := oauthMonitor(t, "cid-imap", "refresh-1", true)
	if err := m.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	f := strings.Fields(<-got)
	raw, _ := base64.StdEncoding.DecodeString(f[1])
	if f[0] != "XOAUTH2" || string(raw) != "user=jane@outlook.com\x01auth=Bearer ACCESS\x01\x01" {
		t.Fatalf("AUTHENTICATE = %v %q", f, raw)
	}
}

func TestConnectWithOAuth2ServerRefusesTheToken(t *testing.T) {
	fakeToken(t, 200, map[string]any{"access_token": "ACCESS", "expires_in": 3600})
	m, _ := oauthMonitor(t, "cid-refused", "refresh-2", false)
	if err := m.Connect(context.Background()); err == nil || !strings.Contains(err.Error(), "login with OAuth") {
		t.Fatalf("got %v", err)
	}
}

func TestConnectWithOAuth2SignInRevoked(t *testing.T) {
	fakeToken(t, 400, map[string]any{"error": "invalid_grant"})
	m, _ := oauthMonitor(t, "cid-revoked", "refresh-3", true)
	if err := m.Connect(context.Background()); err == nil || !strings.Contains(err.Error(), "oauth login") {
		t.Fatalf("got %v", err)
	}
}

// oauthMonitor is a Monitor against a scripted TLS-upgrading IMAP server that
// advertises XOAUTH2 and answers AUTHENTICATE with OK (accept) or NO. got
// receives "XOAUTH2 <initial response>".
func oauthMonitor(t *testing.T, clientID, refresh string, accept bool) (*Monitor, <-chan string) {
	t.Helper()
	ts := httptest.NewTLSServer(http.NotFoundHandler())
	t.Cleanup(ts.Close)
	got := make(chan string, 2)
	addr := fakeIMAPServer(t, func(conn net.Conn, br *bufio.Reader) error {
		inTLS := false
		if err := writeLines(conn, "* OK [CAPABILITY IMAP4rev1 STARTTLS] ready"); err != nil {
			return err
		}
		for {
			tag, rest, err := readCommandLine(br)
			if err != nil {
				return err
			}
			switch cmd := strings.ToUpper(strings.Fields(rest)[0]); cmd {
			case "CAPABILITY":
				caps := "IMAP4rev1 STARTTLS"
				if inTLS {
					caps = "IMAP4rev1 AUTH=XOAUTH2"
				}
				err = writeLines(conn, "* CAPABILITY "+caps, tag+" OK done")
			case "STARTTLS":
				if err = writeLines(conn, tag+" OK begin"); err != nil {
					return err
				}
				tlsConn := tls.Server(conn, &tls.Config{Certificates: ts.TLS.Certificates})
				if err = tlsConn.Handshake(); err != nil {
					return err
				}
				conn, br, inTLS = tlsConn, bufio.NewReader(tlsConn), true
			case "AUTHENTICATE":
				f := strings.Fields(rest)
				ir := ""
				if len(f) > 2 {
					ir = f[2]
				} else {
					if err = writeLines(conn, "+ "); err != nil {
						return err
					}
					line, rerr := br.ReadString('\n')
					if rerr != nil {
						return rerr
					}
					ir = strings.TrimSpace(line)
				}
				got <- f[1] + " " + ir
				if accept {
					return writeLines(conn, tag+" OK authenticated")
				}
				// stay up: the client's Logout after the failure needs an answer
				err = writeLines(conn, tag+" NO AUTHENTICATE failed")
			default:
				err = writeLines(conn, tag+" BAD unexpected "+cmd)
			}
			if err != nil {
				return err
			}
		}
	})
	host, portStr, _ := net.SplitHostPort(addr)
	port, _ := strconv.Atoi(portStr)

	return NewMonitor(config.InboxConfig{Server: host, Port: port, Email: "jane@outlook.com", Password: refresh, Auth: config.AuthOAuth2, ClientID: clientID}, nil), got
}
