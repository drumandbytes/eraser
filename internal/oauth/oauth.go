// Package oauth signs in to mail providers that no longer accept passwords
// (Outlook.com, Hotmail, Live) with the OAuth2 device-code flow and turns the
// result into SASL XOAUTH2 for SMTP and IMAP.
//
// The client is a public one: no client secret, so a user can register their
// own app and put its client_id in the config.
package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Endpoints are a provider's device-code and token URLs plus the scopes to ask for.
// Tests point them at a fake server.
type Endpoints struct {
	DeviceCode string
	Token      string
	Scopes     []string
}

// MicrosoftConsumers is Outlook.com / Hotmail / Live. IMAP and SMTP share one
// resource, so a single token covers both.
var MicrosoftConsumers = Endpoints{
	DeviceCode: "https://login.microsoftonline.com/consumers/oauth2/v2.0/devicecode",
	Token:      "https://login.microsoftonline.com/consumers/oauth2/v2.0/token",
	Scopes: []string{
		"https://outlook.office.com/IMAP.AccessAsUser.All",
		"https://outlook.office.com/SMTP.Send",
		"offline_access",
	},
}

// ErrReauth means the refresh token is no longer valid and the user has to
// run the sign-in again.
var ErrReauth = errors.New("sign-in expired or was revoked: run `delistd oauth login` again")

// DeviceCode is what the user needs to complete a sign-in on another device.
type DeviceCode struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
	Message         string `json:"message"`
}

// Token is a successful token response.
type Token struct {
	Access  string
	Refresh string
	Expires time.Time
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	Error        string `json:"error"`
	Description  string `json:"error_description"`
}

// Client talks to one provider's token service.
type Client struct {
	Endpoints Endpoints
	ClientID  string
	HTTP      *http.Client
	now       func() time.Time
	sleep     func(context.Context, time.Duration) error
}

func NewClient(ep Endpoints, clientID string) *Client {
	return &Client{Endpoints: ep, ClientID: clientID, HTTP: &http.Client{Timeout: 30 * time.Second}}
}

func (c *Client) clock() time.Time {
	if c.now != nil {
		return c.now()
	}
	return time.Now()
}

func (c *Client) wait(ctx context.Context, d time.Duration) error {
	if c.sleep != nil {
		return c.sleep(ctx, d)
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func (c *Client) post(ctx context.Context, endpoint string, form url.Values, out any) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return resp.StatusCode, fmt.Errorf("unexpected response from %s (HTTP %d)", req.URL.Host, resp.StatusCode)
	}
	return resp.StatusCode, nil
}

// StartDeviceCode asks the provider for a code the user enters at the
// verification URL.
func (c *Client) StartDeviceCode(ctx context.Context) (DeviceCode, error) {
	var dc struct {
		DeviceCode
		Error       string `json:"error"`
		Description string `json:"error_description"`
	}
	form := url.Values{"client_id": {c.ClientID}, "scope": {strings.Join(c.Endpoints.Scopes, " ")}}
	if _, err := c.post(ctx, c.Endpoints.DeviceCode, form, &dc); err != nil {
		return DeviceCode{}, err
	}
	if dc.Error != "" || dc.DeviceCode.DeviceCode == "" {
		return DeviceCode{}, fmt.Errorf("could not start sign-in: %s", firstLine(dc.Error, dc.Description))
	}
	if dc.Interval <= 0 {
		dc.Interval = 5
	}
	return dc.DeviceCode, nil
}

// PollDeviceCode waits for the user to finish signing in.
func (c *Client) PollDeviceCode(ctx context.Context, dc DeviceCode) (Token, error) {
	interval := time.Duration(dc.Interval) * time.Second
	deadline := c.clock().Add(time.Duration(dc.ExpiresIn) * time.Second)
	for {
		if err := c.wait(ctx, interval); err != nil {
			return Token{}, err
		}
		if dc.ExpiresIn > 0 && c.clock().After(deadline) {
			return Token{}, errors.New("the sign-in code expired: start again")
		}
		var tr tokenResponse
		form := url.Values{
			"client_id":   {c.ClientID},
			"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
			"device_code": {dc.DeviceCode},
		}
		if _, err := c.post(ctx, c.Endpoints.Token, form, &tr); err != nil {
			return Token{}, err
		}
		switch tr.Error {
		case "":
			return c.token(tr), nil
		case "authorization_pending":
		case "slow_down":
			interval += 5 * time.Second
		case "authorization_declined":
			return Token{}, errors.New("sign-in was declined")
		case "expired_token":
			return Token{}, errors.New("the sign-in code expired: start again")
		default:
			return Token{}, fmt.Errorf("sign-in failed: %s", firstLine(tr.Error, tr.Description))
		}
	}
}

func (c *Client) token(tr tokenResponse) Token {
	return Token{Access: tr.AccessToken, Refresh: tr.RefreshToken, Expires: c.clock().Add(time.Duration(tr.ExpiresIn) * time.Second)}
}

// Refresh trades a refresh token for a fresh access token. The provider may
// return a new refresh token; keep it, it restarts the expiry window.
func (c *Client) Refresh(ctx context.Context, refresh string) (Token, error) {
	var tr tokenResponse
	form := url.Values{
		"client_id":     {c.ClientID},
		"grant_type":    {"refresh_token"},
		"refresh_token": {refresh},
		"scope":         {strings.Join(c.Endpoints.Scopes, " ")},
	}
	code, err := c.post(ctx, c.Endpoints.Token, form, &tr)
	if err != nil {
		return Token{}, err
	}
	switch {
	case tr.Error == "invalid_grant" || tr.Error == "interaction_required":
		return Token{}, ErrReauth
	case tr.Error != "":
		return Token{}, fmt.Errorf("token refresh failed: %s", firstLine(tr.Error, tr.Description))
	case code != http.StatusOK || tr.AccessToken == "":
		return Token{}, fmt.Errorf("token refresh failed (HTTP %d)", code)
	}
	t := c.token(tr)
	if t.Refresh == "" {
		t.Refresh = refresh
	}
	return t, nil
}

func firstLine(code, desc string) string {
	if i := strings.IndexAny(desc, "\r\n"); i >= 0 {
		desc = desc[:i]
	}
	if desc == "" {
		return code
	}
	return code + ": " + desc
}

// Source hands out access tokens for one account, refreshing when they are
// close to expiry. OnRotate is called with a refresh token that differs from
// the stored one so the caller can persist it.
type Source struct {
	Client   *Client
	OnRotate func(newRefresh string) error

	mu      sync.Mutex
	refresh string
	token   Token
}

func NewSource(c *Client, refresh string, onRotate func(string) error) *Source {
	return &Source{Client: c, refresh: refresh, OnRotate: onRotate}
}

// Access returns a valid access token.
func (s *Source) Access(ctx context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.token.Access != "" && s.Client.clock().Add(time.Minute).Before(s.token.Expires) {
		return s.token.Access, nil
	}
	t, err := s.Client.Refresh(ctx, s.refresh)
	if err != nil {
		return "", err
	}
	if t.Refresh != s.refresh {
		s.refresh = t.Refresh
		if s.OnRotate != nil {
			if err := s.OnRotate(t.Refresh); err != nil {
				return "", fmt.Errorf("signed in, but could not save the new token: %w", err)
			}
		}
	}
	s.token = t
	return t.Access, nil
}

// XOAuth2 builds the SASL XOAUTH2 initial response.
func XOAuth2(user, accessToken string) string {
	return "user=" + user + "\x01auth=Bearer " + accessToken + "\x01\x01"
}
