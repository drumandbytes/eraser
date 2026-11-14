package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/drumandbytes/delistd/internal/config"
	"github.com/drumandbytes/delistd/internal/oauth"
)

func fakeMicrosoft(t *testing.T, refresh string) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/devicecode", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"device_code": "dc", "user_code": "ABCD", "verification_uri": "https://example.test/device", "expires_in": 900, "interval": 1})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, _ *http.Request) {
		body := map[string]any{"access_token": "at", "expires_in": 3600}
		if refresh != "" {
			body["refresh_token"] = refresh
		}
		_ = json.NewEncoder(w).Encode(body)
	})
	srv := httptest.NewServer(mux)
	old := oauth.MicrosoftConsumers
	oauth.MicrosoftConsumers = oauth.Endpoints{DeviceCode: srv.URL + "/devicecode", Token: srv.URL + "/token", Scopes: old.Scopes}
	t.Cleanup(func() { srv.Close(); oauth.MicrosoftConsumers = old })
}

func writeBaseConfig(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	cfg := &config.Config{
		Profiles: []config.NamedProfile{{ID: "default", Profile: config.Profile{FirstName: "Jane", LastName: "Doe", Email: "jane@outlook.com"}}},
		Email:    config.EmailConfig{From: "jane@outlook.com"},
	}
	if err := config.Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestOAuthLoginStoresTheSignInForSendingAndInbox(t *testing.T) {
	fakeMicrosoft(t, "REFRESH-1")
	path := writeBaseConfig(t)
	var out bytes.Buffer
	if err := runOAuthLogin(context.Background(), &out, path, "client-1", ""); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "ABCD") || !strings.Contains(out.String(), "example.test/device") {
		t.Fatalf("output lacks the code: %q", out.String())
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	s, in := cfg.Email.SMTP, cfg.Inbox
	if s.Host != "smtp-mail.outlook.com" || s.Port != 587 || s.Auth != config.AuthOAuth2 || s.ClientID != "client-1" || s.Username != "jane@outlook.com" || s.Password != "REFRESH-1" {
		t.Fatalf("smtp = %+v", s)
	}
	if !in.Enabled || in.Server != "outlook.office365.com" || in.Port != 993 || in.Auth != config.AuthOAuth2 || in.Password != "REFRESH-1" || in.Folder != "INBOX" {
		t.Fatalf("inbox = %+v", in)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("config invalid after login: %v", err)
	}
	if err := cfg.ValidateInbox(); err != nil {
		t.Fatalf("inbox invalid after login: %v", err)
	}
}

func TestOAuthLoginErrors(t *testing.T) {
	path := writeBaseConfig(t)
	t.Setenv(clientIDEnv, "")
	if err := runOAuthLogin(context.Background(), &bytes.Buffer{}, path, "", ""); err == nil || !strings.Contains(err.Error(), "client id") {
		t.Fatalf("no client id: %v", err)
	}
	if err := runOAuthLogin(context.Background(), &bytes.Buffer{}, filepath.Join(t.TempDir(), "missing.yaml"), "c", "a@b"); err == nil || !strings.Contains(err.Error(), "init") {
		t.Fatalf("no config: %v", err)
	}
	fakeMicrosoft(t, "")
	if err := runOAuthLogin(context.Background(), &bytes.Buffer{}, path, "c", ""); err == nil || !strings.Contains(err.Error(), "refresh token") {
		t.Fatalf("no refresh token: %v", err)
	}
}

func TestOAuthLoginUsesEnvClientIDAndFlagEmail(t *testing.T) {
	fakeMicrosoft(t, "R")
	t.Setenv(clientIDEnv, "env-client")
	path := writeBaseConfig(t)
	if err := runOAuthLogin(context.Background(), &bytes.Buffer{}, path, "", "other@hotmail.com"); err != nil {
		t.Fatal(err)
	}
	cfg, _ := config.Load(path)
	if cfg.Email.SMTP.ClientID != "env-client" || cfg.Email.From != "other@hotmail.com" || cfg.Inbox.Email != "other@hotmail.com" {
		t.Fatalf("%+v %+v", cfg.Email, cfg.Inbox)
	}
}

func TestRotateHookRewritesTheStoredToken(t *testing.T) {
	fakeMicrosoft(t, "R-old")
	path := writeBaseConfig(t)
	if err := runOAuthLogin(context.Background(), &bytes.Buffer{}, path, "c", ""); err != nil {
		t.Fatal(err)
	}
	prev := cfgFile
	cfgFile = path
	t.Cleanup(func() { cfgFile = prev })

	if err := oauth.RotateHook("R-old", "R-new"); err != nil {
		t.Fatal(err)
	}
	cfg, _ := config.Load(path)
	if cfg.Email.SMTP.Password != "R-new" || cfg.Inbox.Password != "R-new" {
		t.Fatalf("not rotated: %q %q", cfg.Email.SMTP.Password, cfg.Inbox.Password)
	}
	// a token the config no longer holds is left alone
	if err := oauth.RotateHook("unknown", "x"); err != nil {
		t.Fatal(err)
	}
	if cfg2, _ := config.Load(path); cfg2.Email.SMTP.Password != "R-new" {
		t.Fatal("config changed for an unknown token")
	}
	cfgFile = filepath.Join(t.TempDir(), "nope.yaml")
	if err := oauth.RotateHook("a", "b"); err == nil {
		t.Fatal("expected load error")
	}
}

func TestOAuthCmdRegistered(t *testing.T) {
	c, _, err := newRootCmd().Find([]string{"oauth", "login"})
	if err != nil || c.Name() != "login" {
		t.Fatalf("%v %v", c, err)
	}
}

func TestOAuthLoginFailurePaths(t *testing.T) {
	path := writeBaseConfig(t)
	// Microsoft refuses to start the sign-in
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(400)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized_client"})
	}))
	t.Cleanup(srv.Close)
	old := oauth.MicrosoftConsumers
	oauth.MicrosoftConsumers = oauth.Endpoints{DeviceCode: srv.URL, Token: srv.URL, Scopes: old.Scopes}
	t.Cleanup(func() { oauth.MicrosoftConsumers = old })
	if err := runOAuthLogin(context.Background(), &bytes.Buffer{}, path, "c", ""); err == nil || !strings.Contains(err.Error(), "unauthorized_client") {
		t.Fatalf("start refused: %v", err)
	}

	// the user never finishes: the poll is cancelled
	fakeMicrosoft(t, "")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := runOAuthLogin(ctx, &bytes.Buffer{}, path, "c", ""); err == nil {
		t.Fatal("expected cancel")
	}

	// no address anywhere
	cfg, _ := config.Load(path)
	cfg.Email.From = ""
	if err := config.Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	if err := runOAuthLogin(context.Background(), &bytes.Buffer{}, path, "c", ""); err == nil || !strings.Contains(err.Error(), "--email") {
		t.Fatalf("no address: %v", err)
	}
}

func TestOAuthLoginCommandRuns(t *testing.T) {
	fakeMicrosoft(t, "R")
	path := writeBaseConfig(t)
	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"--config", path, "oauth", "login", "--client-id", "cid", "--email", "jane@outlook.com"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Signed in as jane@outlook.com") {
		t.Fatalf("output: %q", out.String())
	}
}
