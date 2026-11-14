package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"

	"github.com/spf13/cobra"

	"github.com/drumandbytes/delistd/internal/config"
	"github.com/drumandbytes/delistd/internal/oauth"
)

// ClientIDEnv names the OAuth app id used when --client-id and the config have none.
const clientIDEnv = "DELISTD_OAUTH_CLIENT_ID"

func init() {
	// A rotated refresh token is written back to the config file, so a
	// long-running process keeps working past the old token's expiry.
	oauth.RotateHook = func(oldRefresh, newRefresh string) error {
		path := resolveConfigPath()
		cfg, err := config.Load(path)
		if err != nil {
			return err
		}
		if !cfg.ReplaceSecret(oldRefresh, newRefresh) {
			return nil // config changed since; the in-memory token still works until it expires
		}
		return config.Save(path, cfg)
	}
}

func oauthCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "oauth",
		Short: "Sign in to mail providers that no longer accept passwords (Outlook.com)",
	}
	var clientID, email string
	login := &cobra.Command{
		Use:   "login",
		Short: "Sign in to Outlook.com / Hotmail / Live with a code, and store the sign-in",
		Long: `Microsoft no longer accepts app passwords for Outlook.com, Hotmail and Live,
so Delistd signs in the way Microsoft requires: it shows a short code, you
enter it at microsoft.com/devicelogin, and Delistd stores the resulting
sign-in (encrypted, like a password) as both your sending and your inbox account.

You need your own free app registration for the client id; see docs/outlook.md.
Run 'delistd init' first so a config exists.`,
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
			defer stop()
			return runOAuthLogin(ctx, cmd.OutOrStdout(), resolveConfigPath(), clientID, email)
		},
	}
	login.Flags().StringVar(&clientID, "client-id", "", "Application (client) ID of your Microsoft app registration")
	login.Flags().StringVar(&email, "email", "", "Your Outlook.com address (default: the config's From address)")
	cmd.AddCommand(login)
	return cmd
}

func runOAuthLogin(ctx context.Context, out io.Writer, path, clientID, email string) error {
	cfg, err := config.Load(path)
	if err != nil {
		return fmt.Errorf("failed to load config (run `delistd init` first): %w", err)
	}
	if clientID == "" {
		clientID = firstNonEmpty(cfg.Email.SMTP.ClientID, cfg.Inbox.ClientID, os.Getenv(clientIDEnv))
	}
	if clientID == "" {
		return fmt.Errorf("a client id is required: pass --client-id (see docs/outlook.md for how to register the app)")
	}
	email = strings.TrimSpace(firstNonEmpty(email, cfg.Email.From))
	if email == "" {
		return fmt.Errorf("no address to sign in: pass --email")
	}

	c := oauth.NewClient(oauth.MicrosoftConsumers, clientID)
	dc, err := c.StartDeviceCode(ctx)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "To sign in, open %s and enter the code %s.\nWaiting for you to finish (Ctrl+C to cancel)...\n", dc.VerificationURI, dc.UserCode)
	tok, err := c.PollDeviceCode(ctx, dc)
	if err != nil {
		return err
	}
	if tok.Refresh == "" {
		return fmt.Errorf("Microsoft returned no refresh token, so unattended runs would stop after an hour; check that the app allows the offline_access permission")
	}

	p := config.OutlookOAuth
	cfg.Email.From = email
	cfg.Email.SMTP = config.SMTPConfig{Host: p.SMTPHost, Port: p.SMTPPort, Username: email, Password: tok.Refresh, Auth: config.AuthOAuth2, ClientID: clientID}
	cfg.Inbox = config.InboxConfig{
		Enabled: true, Provider: p.ID, Server: p.IMAPHost, Port: p.IMAPPort, Email: email, Password: tok.Refresh,
		Auth: config.AuthOAuth2, ClientID: clientID,
		Folder: cfg.Inbox.Folder, AutoArchive: cfg.Inbox.AutoArchive, ArchiveFolder: cfg.Inbox.ArchiveFolder,
	}
	config.ApplyInboxDefaults(&cfg.Inbox)
	if err := config.Save(path, cfg); err != nil {
		return err
	}
	fmt.Fprintf(out, "Signed in as %s. Sending and the inbox now use this sign-in.\n", email)
	return nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
