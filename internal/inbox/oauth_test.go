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

func TestConnectWithOAuth2(t *testing.T) {
	tok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "ACCESS", "expires_in": 3600})
	}))
	t.Cleanup(tok.Close)
	old := oauth.MicrosoftConsumers
	oauth.MicrosoftConsumers = oauth.Endpoints{Token: tok.URL, Scopes: old.Scopes}
	t.Cleanup(func() { oauth.MicrosoftConsumers = old })

	ts := httptest.NewTLSServer(http.NotFoundHandler())
	t.Cleanup(ts.Close)
	got := make(chan string, 1)
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
				return writeLines(conn, tag+" OK authenticated")
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

	m := NewMonitor(config.InboxConfig{Server: host, Port: port, Email: "jane@outlook.com", Password: "refresh-1", Auth: config.AuthOAuth2, ClientID: "cid-imap"}, nil)
	if err := m.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	f := strings.Fields(<-got)
	raw, _ := base64.StdEncoding.DecodeString(f[1])
	if f[0] != "XOAUTH2" || string(raw) != "user=jane@outlook.com\x01auth=Bearer ACCESS\x01\x01" {
		t.Fatalf("AUTHENTICATE = %v %q", f, raw)
	}
}
