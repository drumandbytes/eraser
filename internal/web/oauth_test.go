package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/drumandbytes/delistd/internal/config"
	"github.com/drumandbytes/delistd/internal/oauth"
)

func fakeMicrosoft(t *testing.T, tokenErr string) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/devicecode", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"device_code": "dc", "user_code": "WXYZ", "verification_uri": "https://example.test/device", "expires_in": 900, "interval": 1})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, _ *http.Request) {
		if tokenErr != "" {
			w.WriteHeader(400)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": tokenErr})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "at", "refresh_token": "REFRESH-WEB", "expires_in": 3600})
	})
	srv := httptest.NewServer(mux)
	old := oauth.MicrosoftConsumers
	oauth.MicrosoftConsumers = oauth.Endpoints{DeviceCode: srv.URL + "/devicecode", Token: srv.URL + "/token", Scopes: old.Scopes}
	t.Cleanup(func() { srv.Close(); oauth.MicrosoftConsumers = old })
}

var oauthIDRe = regexp.MustCompile(`name="oauth_id" value="([^"]+)"`)
var pollRe = regexp.MustCompile(`/oauth/status\?id=([^"]+)"`)

func (c *wizardClient) postHTMX(path string, form url.Values) string {
	c.t.Helper()
	return readBody(c.t, c.post(path, form))
}

func TestSetupWizardOutlookOAuth(t *testing.T) {
	fakeMicrosoft(t, "")
	c, s := newWizardClient(t)
	c.get("/setup/profile")
	c.post("/setup/profile", url.Values{"first_name": {"Ada"}, "last_name": {"Lovelace"}, "email": {"ada@outlook.com"}, "country": {"United Kingdom"}})

	page := c.get("/setup/email")
	for _, want := range []string{`<option value="outlook"`, "Application (client) ID", "Sign in with Microsoft", "Personal Microsoft accounts only"} {
		if !strings.Contains(page, want) {
			t.Fatalf("email step lacks %q", want)
		}
	}

	// no client id: told what to do, nothing started
	if body := c.postHTMX("/oauth/start", url.Values{}); !strings.Contains(body, "client) ID first") {
		t.Fatalf("missing client id not explained: %s", body)
	}

	body := c.postHTMX("/oauth/start", url.Values{"oauth_client_id": {"cid-web"}})
	if !strings.Contains(body, "WXYZ") || !strings.Contains(body, "example.test/device") {
		t.Fatalf("code not shown: %s", body)
	}
	m := pollRe.FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("no polling hook: %s", body)
	}
	id := m[1]

	var done string
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
		if done = c.get("/oauth/status?id=" + id); strings.Contains(done, `name="oauth_id"`) {
			break
		}
	}
	if om := oauthIDRe.FindStringSubmatch(done); om == nil || om[1] != id {
		t.Fatalf("sign-in did not finish: %s", done)
	}
	if strings.Contains(done, "REFRESH-WEB") {
		t.Fatal("the refresh token must never reach the browser")
	}

	// posting without the sign-in is refused; with it, the step saves
	resp := c.post("/setup/email", url.Values{"mail_provider": {"outlook"}, "mail_address": {"ada@outlook.com"}, "oauth_client_id": {"cid-web"}})
	if b := readBody(t, resp); resp.StatusCode != http.StatusOK || !strings.Contains(b, "Sign in with Microsoft first") {
		t.Fatalf("unsigned post: %d %s", resp.StatusCode, b)
	}
	resp = c.post("/setup/email", url.Values{"mail_provider": {"outlook"}, "mail_address": {"ada@outlook.com"}, "oauth_client_id": {"cid-web"}, "oauth_id": {id}})
	if resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != "/setup/test" {
		t.Fatalf("signed post: %d -> %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	// a sign-in made for another client id is not accepted
	resp = c.post("/setup/email", url.Values{"mail_provider": {"outlook"}, "mail_address": {"ada@outlook.com"}, "oauth_client_id": {"someone-elses"}, "oauth_id": {id}})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("mismatched client id accepted: %d", resp.StatusCode)
	}

	c.get("/setup/test")
	if resp, err := c.http.Get(c.srv.URL + "/setup/complete"); err != nil {
		t.Fatal(err)
	} else if b := readBody(t, resp); resp.StatusCode != http.StatusOK {
		t.Fatalf("complete: %d %s", resp.StatusCode, b)
	}
	saved, err := config.Load(s.configPath)
	if err != nil {
		t.Fatal(err)
	}
	e := saved.Email.SMTP
	if e.Auth != config.AuthOAuth2 || e.ClientID != "cid-web" || e.Password != "REFRESH-WEB" || e.Host != "smtp-mail.outlook.com" || e.Port != 587 {
		t.Fatalf("saved smtp = %+v", e)
	}
	if err := saved.Validate(); err != nil {
		t.Fatalf("saved config invalid: %v", err)
	}
}

func TestOAuthStatusFailuresAndExpiry(t *testing.T) {
	fakeMicrosoft(t, "authorization_declined")
	c, _ := newWizardClient(t)
	c.get("/setup/profile")
	body := c.postHTMX("/oauth/start", url.Values{"oauth_client_id": {"cid"}})
	id := pollRe.FindStringSubmatch(body)[1]
	var got string
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
		if got = c.get("/oauth/status?id=" + id); strings.Contains(got, "declined") {
			break
		}
	}
	if !strings.Contains(got, "declined") || strings.Contains(got, `name="oauth_id"`) {
		t.Fatalf("declined sign-in: %s", got)
	}
	if got := c.get("/oauth/status?id=nope"); !strings.Contains(got, "expired") {
		t.Fatalf("unknown id: %s", got)
	}
}

func TestOAuthStartRejectedByMicrosoft(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(400)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized_client", "error_description": "app not found"})
	}))
	t.Cleanup(srv.Close)
	old := oauth.MicrosoftConsumers
	oauth.MicrosoftConsumers = oauth.Endpoints{DeviceCode: srv.URL, Token: srv.URL, Scopes: old.Scopes}
	t.Cleanup(func() { oauth.MicrosoftConsumers = old })
	c, _ := newWizardClient(t)
	c.get("/setup/profile")
	if body := c.postHTMX("/oauth/start", url.Values{"oauth_client_id": {"bad"}}); !strings.Contains(body, "unauthorized_client") {
		t.Fatalf("Microsoft's reason not shown: %s", body)
	}
}

func TestMailFormOAuthRoundTrip(t *testing.T) {
	e := config.EmailConfig{From: "a@outlook.com", SMTP: config.SMTPConfig{Host: "smtp-mail.outlook.com", Port: 587, Username: "a@outlook.com", Password: "tok", Auth: config.AuthOAuth2, ClientID: "cid"}}
	in := &config.InboxConfig{Server: "outlook.office365.com", Port: 993, Email: "a@outlook.com", Password: "tok", Auth: config.AuthOAuth2, ClientID: "cid"}
	f := mailFormFromConfig(e, in)
	v := newMailFormView("both", f, nil)
	if !v.OAuth || v.Provider != "outlook" || v.ClientID != "cid" || !v.SignedIn {
		t.Fatalf("view = %+v", v)
	}
	f.Password = "tok2"
	if ec := f.emailConfig(); ec.SMTP.Auth != config.AuthOAuth2 || ec.SMTP.ClientID != "cid" || ec.SMTP.Password != "tok2" {
		t.Fatalf("emailConfig = %+v", ec)
	}
	if ic := f.inboxConfig(); ic.Auth != config.AuthOAuth2 || ic.ClientID != "cid" || ic.Server != "outlook.office365.com" {
		t.Fatalf("inboxConfig = %+v", ic)
	}
	// saved inbox-only OAuth account still reads as signed in
	if v := newMailFormView("imap", mailFormFromConfig(config.EmailConfig{}, in), nil); !v.OAuth || !v.SignedIn {
		t.Fatalf("inbox-only view = %+v", v)
	}
}

func TestKeepStoredOnlyForTheSameKindOfSignIn(t *testing.T) {
	f := mailForm{Username: "a@outlook.com", OAuth: true, ClientID: "cid"}
	f.keepStored("a@outlook.com", "tok", config.AuthOAuth2, "cid")
	if f.Password != "tok" {
		t.Fatalf("same sign-in not kept: %q", f.Password)
	}
	for _, tc := range []struct{ user, auth, cid string }{
		{"a@outlook.com", "", ""},                      // saved app password is not a refresh token
		{"a@outlook.com", config.AuthOAuth2, "x"},      // other app
		{"someone@else.org", config.AuthOAuth2, "cid"}, // other login
	} {
		g := mailForm{Username: "a@outlook.com", OAuth: true, ClientID: "cid"}
		g.keepStored(tc.user, "tok", tc.auth, tc.cid)
		if g.Password != "" {
			t.Errorf("%+v kept %q", tc, g.Password)
		}
	}
	pw := mailForm{Username: "a@gmail.com"}
	pw.keepStored("a@gmail.com", "apppw", "", "")
	oa := mailForm{Username: "a@gmail.com"}
	oa.keepStored("a@gmail.com", "tok", config.AuthOAuth2, "cid")
	if pw.Password != "apppw" || oa.Password != "" {
		t.Fatalf("password account: %q / %q", pw.Password, oa.Password)
	}
	filled := mailForm{Username: "a@gmail.com", Password: "new"}
	filled.keepStored("a@gmail.com", "old", "", "")
	if filled.Password != "new" {
		t.Fatal("typed password overwritten")
	}
}

func TestValidateOAuthForm(t *testing.T) {
	if e := (mailForm{Address: "a@outlook.com", OAuth: true, SMTPHost: "h", SMTPPort: 587}).validate("smtp", true); e["oauth"] == "" || e["mail_password"] != "" {
		t.Fatalf("no client id: %v", e)
	}
	if e := (mailForm{Address: "a@outlook.com", OAuth: true, ClientID: "c", SMTPHost: "h", SMTPPort: 587}).validate("smtp", true); !strings.Contains(e["oauth"], "first") {
		t.Fatalf("not signed in: %v", e)
	}
	if e := (mailForm{Address: "a@outlook.com", OAuth: true, ClientID: "c", Password: "t", SMTPHost: "h", SMTPPort: 587}).validate("smtp", true); len(e) != 0 {
		t.Fatalf("valid: %v", e)
	}
}

func TestProvidersForFormListsOutlookBeforeOther(t *testing.T) {
	p := providersForForm()
	if p[len(p)-1].ID != "custom" || p[len(p)-2].ID != "outlook" || len(p) != len(config.Providers)+1 {
		t.Fatalf("%v", p)
	}
}

func TestSignInStoreExpiresOldEntries(t *testing.T) {
	st := &signInStore{m: map[string]*signIn{}}
	old := st.add(&signIn{started: time.Now().Add(-2 * signInTTL)})
	st.add(&signIn{started: time.Now()})
	if _, ok := st.get(old); ok {
		t.Fatal("expired sign-in kept")
	}
	if _, ok := st.token("missing", "c"); ok {
		t.Fatal("token for unknown id")
	}
}

func TestOAuthSignInWithoutRefreshToken(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/devicecode", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"device_code": "dc", "user_code": "NORT", "verification_uri": "https://example.test/device", "expires_in": 900, "interval": 1})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "at", "expires_in": 3600})
	})
	srv := httptest.NewServer(mux)
	old := oauth.MicrosoftConsumers
	oauth.MicrosoftConsumers = oauth.Endpoints{DeviceCode: srv.URL + "/devicecode", Token: srv.URL + "/token", Scopes: old.Scopes}
	t.Cleanup(func() { srv.Close(); oauth.MicrosoftConsumers = old })

	c, _ := newWizardClient(t)
	c.get("/setup/profile")
	id := pollRe.FindStringSubmatch(c.postHTMX("/oauth/start", url.Values{"oauth_client_id": {"cid"}}))[1]
	var got string
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
		if got = c.get("/oauth/status?id=" + id); strings.Contains(got, "no refresh token") {
			break
		}
	}
	if !strings.Contains(got, "offline_access") || strings.Contains(got, `name="oauth_id"`) {
		t.Fatalf("got %s", got)
	}
}

func TestMailFormViewFillsOutlookServers(t *testing.T) {
	v := newMailFormView("both", mailForm{Provider: "outlook"}, nil)
	if v.SMTPHost != "smtp-mail.outlook.com" || v.SMTPPort != 587 || v.IMAPHost != "outlook.office365.com" || v.IMAPPort != 993 || !v.OAuth {
		t.Fatalf("%+v", v)
	}
	// hosts already on the form are kept
	v = newMailFormView("both", mailForm{Provider: "outlook", SMTPHost: "x", IMAPHost: "y"}, nil)
	if v.SMTPHost != "x" || v.IMAPHost != "y" {
		t.Fatalf("%+v", v)
	}
}
