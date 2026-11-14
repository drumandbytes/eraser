package oauth

import (
	"context"
	"net/smtp"
	"sync"
)

// RotateHook persists a refresh token the provider replaced. Set once at
// startup by whatever owns the config file; nil means rotations are kept in
// memory only (tests, one-shot tools).
var RotateHook func(oldRefresh, newRefresh string) error

var (
	sourcesMu sync.Mutex
	sources   = map[string]*Source{}
)

// AccessFor returns an access token for the account signed in with refresh,
// sharing one Source (and so one refresh) across the SMTP sender and the IMAP
// monitor within a process.
func AccessFor(ctx context.Context, clientID, refresh string) (string, error) {
	key := clientID + "\x00" + refresh
	sourcesMu.Lock()
	s, ok := sources[key]
	if !ok {
		s = NewSource(NewClient(MicrosoftConsumers, clientID), refresh, func(n string) error {
			if RotateHook != nil {
				return RotateHook(refresh, n)
			}
			return nil
		})
		sources[key] = s
	}
	sourcesMu.Unlock()
	return s.Access(ctx)
}

// SASL is a SASL XOAUTH2 client, usable by net/smtp (via SMTPAuth) and by
// go-imap's Authenticate.
type SASL struct{ User, Token string }

func (s SASL) Start() (string, []byte, error) {
	return "XOAUTH2", []byte(XOAuth2(s.User, s.Token)), nil
}

// Next answers the server's error challenge with an empty line, as XOAUTH2
// requires, so the server can send its real failure status.
func (s SASL) Next([]byte) ([]byte, error) { return []byte{}, nil }

type smtpAuth struct{ SASL }

func (a smtpAuth) Start(*smtp.ServerInfo) (string, []byte, error) { return a.SASL.Start() }
func (a smtpAuth) Next(_ []byte, more bool) ([]byte, error) {
	if more {
		return []byte{}, nil
	}
	return nil, nil
}

// SMTPAuth is XOAUTH2 for net/smtp.
func SMTPAuth(user, token string) smtp.Auth { return smtpAuth{SASL{user, token}} }
