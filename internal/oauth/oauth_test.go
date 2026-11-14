package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func fake(t *testing.T, token func(r *http.Request) (int, any)) (*Client, *httptest.Server) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/devicecode", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.Form.Get("client_id") != "cid" || !strings.Contains(r.Form.Get("scope"), "SMTP.Send") {
			http.Error(w, `{"error":"invalid_request"}`, 400)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"device_code": "dc", "user_code": "ABC", "verification_uri": "https://x/device", "expires_in": 900, "interval": 1})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		code, body := token(r)
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(body)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c := NewClient(Endpoints{DeviceCode: srv.URL + "/devicecode", Token: srv.URL + "/token", Scopes: MicrosoftConsumers.Scopes}, "cid")
	c.sleep = func(context.Context, time.Duration) error { return nil }
	return c, srv
}

func TestDeviceCodeFlow(t *testing.T) {
	polls := 0
	c, _ := fake(t, func(r *http.Request) (int, any) {
		if r.Form.Get("device_code") != "dc" {
			return 400, map[string]string{"error": "bad"}
		}
		polls++
		switch polls {
		case 1:
			return 400, map[string]string{"error": "authorization_pending"}
		case 2:
			return 400, map[string]string{"error": "slow_down"}
		}
		return 200, map[string]any{"access_token": "at", "refresh_token": "rt", "expires_in": 3600}
	})
	dc, err := c.StartDeviceCode(context.Background())
	if err != nil || dc.UserCode != "ABC" {
		t.Fatalf("start: %+v %v", dc, err)
	}
	tok, err := c.PollDeviceCode(context.Background(), dc)
	if err != nil || tok.Access != "at" || tok.Refresh != "rt" || polls != 3 {
		t.Fatalf("poll: %+v %v polls=%d", tok, err, polls)
	}
}

func TestPollFailures(t *testing.T) {
	for _, tc := range []struct{ code, want string }{
		{"authorization_declined", "declined"},
		{"expired_token", "expired"},
		{"weird", "weird"},
	} {
		c, _ := fake(t, func(*http.Request) (int, any) { return 400, map[string]string{"error": tc.code} })
		_, err := c.PollDeviceCode(context.Background(), DeviceCode{DeviceCode: "dc", Interval: 1, ExpiresIn: 900})
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v", tc.code, err)
		}
	}
}

func TestPollLocalExpiry(t *testing.T) {
	c, _ := fake(t, func(*http.Request) (int, any) { return 400, map[string]string{"error": "authorization_pending"} })
	base := time.Now()
	n := 0
	c.now = func() time.Time { n++; return base.Add(time.Duration(n) * time.Hour) }
	_, err := c.PollDeviceCode(context.Background(), DeviceCode{DeviceCode: "dc", Interval: 1, ExpiresIn: 60})
	if err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("got %v", err)
	}
}

func TestStartRejected(t *testing.T) {
	c, _ := fake(t, nil)
	c.ClientID = "other"
	if _, err := c.StartDeviceCode(context.Background()); err == nil {
		t.Fatal("expected error")
	}
}

func TestRefreshAndSource(t *testing.T) {
	calls := 0
	c, _ := fake(t, func(r *http.Request) (int, any) {
		calls++
		if r.Form.Get("refresh_token") == "dead" {
			return 400, map[string]string{"error": "invalid_grant", "error_description": "AADSTS70000\r\ntrace"}
		}
		return 200, map[string]any{"access_token": "at" + r.Form.Get("refresh_token"), "refresh_token": "new", "expires_in": 3600}
	})
	var rotated []string
	s := NewSource(c, "old", func(n string) error { rotated = append(rotated, n); return nil })
	for i := 0; i < 2; i++ {
		got, err := s.Access(context.Background())
		if err != nil || got != "atold" {
			t.Fatalf("access: %q %v", got, err)
		}
	}
	if calls != 1 || len(rotated) != 1 || rotated[0] != "new" {
		t.Fatalf("calls=%d rotated=%v", calls, rotated)
	}
	// expired cache refreshes again, with the rotated token, no further rotation
	s.token.Expires = time.Now()
	if got, _ := s.Access(context.Background()); got != "atnew" || len(rotated) != 1 {
		t.Fatalf("got %q rotated=%v", got, rotated)
	}
	if _, err := NewSource(c, "dead", nil).Access(context.Background()); !errors.Is(err, ErrReauth) {
		t.Fatalf("want ErrReauth, got %v", err)
	}
}

func TestSourceRotateSaveFails(t *testing.T) {
	c, _ := fake(t, func(*http.Request) (int, any) {
		return 200, map[string]any{"access_token": "a", "refresh_token": "n", "expires_in": 3600}
	})
	s := NewSource(c, "o", func(string) error { return errors.New("disk full") })
	if _, err := s.Access(context.Background()); err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("got %v", err)
	}
}

func TestRefreshKeepsOldTokenWhenNoneReturned(t *testing.T) {
	c, _ := fake(t, func(*http.Request) (int, any) { return 200, map[string]any{"access_token": "a", "expires_in": 60} })
	tok, err := c.Refresh(context.Background(), "keep")
	if err != nil || tok.Refresh != "keep" {
		t.Fatalf("%+v %v", tok, err)
	}
}

func TestRefreshOtherErrors(t *testing.T) {
	c, _ := fake(t, func(*http.Request) (int, any) { return 500, map[string]string{"error": "server_error"} })
	if _, err := c.Refresh(context.Background(), "r"); err == nil || !strings.Contains(err.Error(), "server_error") {
		t.Fatalf("got %v", err)
	}
	c, _ = fake(t, func(*http.Request) (int, any) { return 503, map[string]any{} })
	if _, err := c.Refresh(context.Background(), "r"); err == nil || !strings.Contains(err.Error(), "503") {
		t.Fatalf("got %v", err)
	}
}

func TestXOAuth2(t *testing.T) {
	if got := XOAuth2("a@b", "tok"); got != "user=a@b\x01auth=Bearer tok\x01\x01" {
		t.Fatalf("%q", got)
	}
}

func TestWaitCancelled(t *testing.T) {
	c := NewClient(MicrosoftConsumers, "cid")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := c.wait(ctx, time.Hour); err == nil {
		t.Fatal("expected cancel")
	}
	if err := c.wait(context.Background(), time.Millisecond); err != nil {
		t.Fatal(err)
	}
}

func TestAccessForSharesSourceAndRotates(t *testing.T) {
	c, srv := fake(t, func(r *http.Request) (int, any) {
		return 200, map[string]any{"access_token": "at", "refresh_token": "rot", "expires_in": 3600}
	})
	_ = c
	old := MicrosoftConsumers
	MicrosoftConsumers = Endpoints{DeviceCode: srv.URL + "/devicecode", Token: srv.URL + "/token", Scopes: old.Scopes}
	defer func() { MicrosoftConsumers = old; RotateHook = nil }()
	var oldTok, newTok string
	RotateHook = func(o, n string) error { oldTok, newTok = o, n; return nil }
	for i := 0; i < 2; i++ {
		if got, err := AccessFor(context.Background(), "cid", "r0"); err != nil || got != "at" {
			t.Fatalf("%q %v", got, err)
		}
	}
	if oldTok != "r0" || newTok != "rot" {
		t.Fatalf("hook got %q -> %q", oldTok, newTok)
	}
}

func TestSASLAndSMTPAuth(t *testing.T) {
	mech, ir, _ := SASL{"u", "t"}.Start()
	if mech != "XOAUTH2" || string(ir) != "user=u\x01auth=Bearer t\x01\x01" {
		t.Fatalf("%s %q", mech, ir)
	}
	if r, err := (SASL{}).Next([]byte("{}")); err != nil || r == nil || len(r) != 0 {
		t.Fatalf("%v %v", r, err)
	}
	a := SMTPAuth("u", "t")
	if m, _, _ := a.Start(nil); m != "XOAUTH2" {
		t.Fatal(m)
	}
	if r, _ := a.Next(nil, true); r == nil {
		t.Fatal("challenge needs an empty reply")
	}
	if r, _ := a.Next(nil, false); r != nil {
		t.Fatal("no reply when done")
	}
}
