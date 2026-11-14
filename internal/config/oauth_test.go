package config

import (
	"strings"
	"testing"
)

func oauthSMTP() EmailConfig {
	return EmailConfig{From: "jane@outlook.com", SMTP: SMTPConfig{Host: "smtp-mail.outlook.com", Port: 587, Username: "jane@outlook.com", Auth: AuthOAuth2, ClientID: "cid", Password: "refresh"}}
}

func TestValidateEmailOAuth(t *testing.T) {
	if err := validateEmailConfig(oauthSMTP()); err != nil {
		t.Fatalf("valid oauth config rejected: %v", err)
	}
	for _, tc := range []struct {
		name string
		edit func(*EmailConfig)
		want string
	}{
		{"unknown auth", func(e *EmailConfig) { e.SMTP.Auth = "magic" }, "unknown auth"},
		{"no client id", func(e *EmailConfig) { e.SMTP.ClientID = "" }, "client_id"},
		{"not signed in", func(e *EmailConfig) { e.SMTP.Password = "" }, "oauth login"},
	} {
		e := oauthSMTP()
		tc.edit(&e)
		if err := validateEmailConfig(e); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v", tc.name, err)
		}
	}
}

func TestValidateInboxOAuth(t *testing.T) {
	ok := InboxConfig{Enabled: true, Server: "outlook.office365.com", Port: 993, Email: "jane@outlook.com", Auth: AuthOAuth2, ClientID: "cid", Password: "refresh"}
	if err := validateInboxConfig(ok); err != nil {
		t.Fatalf("valid: %v", err)
	}
	for _, tc := range []struct {
		name string
		edit func(*InboxConfig)
		want string
	}{
		{"not signed in", func(i *InboxConfig) { i.Password = "" }, "oauth login"},
		{"unknown auth", func(i *InboxConfig) { i.Auth = "magic" }, "unknown auth"},
		{"no client id", func(i *InboxConfig) { i.ClientID = "" }, "client_id"},
	} {
		i := ok
		tc.edit(&i)
		if err := validateInboxConfig(i); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v", tc.name, err)
		}
	}
	plain := ok
	plain.Auth, plain.Password = "", ""
	if err := validateInboxConfig(plain); err == nil || !strings.Contains(err.Error(), "app password") {
		t.Errorf("password account without password: %v", err)
	}
}

func TestReplaceSecret(t *testing.T) {
	c := &Config{Email: oauthSMTP(), Inbox: InboxConfig{Password: "refresh"}}
	c.Profiles = []NamedProfile{{ID: "p", Mail: &MailConfig{Inbox: &InboxConfig{Password: "other"}}}}
	if !c.ReplaceSecret("refresh", "next") || c.Email.SMTP.Password != "next" || c.Inbox.Password != "next" || c.Profiles[0].Mail.Inbox.Password != "other" {
		t.Fatalf("%+v", c)
	}
	if c.ReplaceSecret("refresh", "x") {
		t.Fatal("matched a token that was already replaced")
	}
}

func TestOutlookOAuthPresetIsNotInProviders(t *testing.T) {
	if _, ok := ProviderByID("outlook"); ok {
		t.Fatal("the OAuth preset must not appear in Providers: the CLI wizard would ask for a password")
	}
	if !OutlookOAuth.OAuth || OutlookOAuth.SMTPPort != 587 || OutlookOAuth.IMAPPort != 993 {
		t.Fatalf("%+v", OutlookOAuth)
	}
}

func TestReplaceSecretSkipsEmptyPasswords(t *testing.T) {
	c := &Config{}
	if c.ReplaceSecret("anything", "x") {
		t.Fatal("matched an empty password")
	}
}

func withDefaultClientID(t *testing.T, id string) {
	t.Helper()
	old := DefaultOAuthClientID
	DefaultOAuthClientID = id
	t.Cleanup(func() { DefaultOAuthClientID = old })
}

func TestSharedClientIDIsTheFallback(t *testing.T) {
	if OAuthClientID("") != "" || OAuthClientID("mine") != "mine" {
		t.Fatal("with no default, an empty id stays empty")
	}
	withDefaultClientID(t, "shared")
	if OAuthClientID("") != "shared" || OAuthClientID("mine") != "mine" {
		t.Fatalf("got %q / %q", OAuthClientID(""), OAuthClientID("mine"))
	}
	e := oauthSMTP()
	e.SMTP.ClientID = ""
	if err := validateEmailConfig(e); err != nil {
		t.Fatalf("smtp without client_id rejected despite a default: %v", err)
	}
	in := InboxConfig{Enabled: true, Server: "s", Port: 993, Email: "a@b", Auth: AuthOAuth2, Password: "r"}
	if err := validateInboxConfig(in); err != nil {
		t.Fatalf("inbox without client_id rejected despite a default: %v", err)
	}
}

func TestClientIDPrecedence(t *testing.T) {
	withDefaultClientID(t, "built-in")
	if OAuthClientID("") != "built-in" {
		t.Fatal("default")
	}
	t.Setenv(OAuthClientIDEnv, " from-env ")
	if OAuthClientID("") != "from-env" {
		t.Fatalf("env should beat the built-in default: %q", OAuthClientID(""))
	}
	if OAuthClientID("account") != "account" {
		t.Fatal("an account's own client_id beats the environment")
	}
}
