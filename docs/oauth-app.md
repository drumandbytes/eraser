# The shared Microsoft app (maintainers)

Outlook.com, Hotmail and Live accounts sign in through a Microsoft app registration. Delistd is meant to ship one shared registration so users only click "Sign in with Microsoft"; `config.DefaultOAuthClientID` holds its Application (client) ID. This page is what to know about it, how to register it, and what to do if it is abused. User-facing steps are in [outlook.md](outlook.md).

## What the client ID is, and isn't

It identifies the app and is public by design: every sign-in request carries it and the device-code flow has no client secret. It is plain source on purpose; there is nothing to hide.

Anyone can use it in their own script. That cannot read anyone's mail (a token exists only for a user who approves a prompt), but it can **impersonate Delistd**: an attacker starts a device-code sign-in with our ID and talks a victim into entering the code at Microsoft's page. The consent screen then says "Delistd". The same holds for every shared public client ID. The limits we set:

- **Minimum scopes**: `IMAP.AccessAsUser.All`, `SMTP.Send`, `offline_access`, nothing else. `internal/oauth` has a test (`TestScopesStayMinimal`) that fails if the list changes; change the registration and this page with it.
- **Our own UI never links elsewhere**: the sign-in link must be https on a Microsoft domain (`TrustedHosts`), and both the web form and the CLI warn never to enter a code someone else sent.
- **Branding that identifies us** (below), so a fake prompt at least carries our homepage and privacy links.

## Registration settings

Register in a Drumandbytes-owned tenant, not a personal account (a personal account can't be publisher-verified and may be refused at Entra). Keep credentials and recovery with the org.

- Name: `Delistd`. Supported account types: **Personal Microsoft accounts only**.
- Authentication: **Allow public client flows: Yes**. No redirect URI, no client secret.
- API permissions (delegated): `IMAP.AccessAsUser.All`, `SMTP.Send`, `offline_access`.
- Branding: homepage `https://delistd.dev/`, privacy statement `https://delistd.dev/privacy/`. The terms field is optional; leave it empty or point at the project's licence, whichever the company prefers.
- Owners: at least two people, so the registration can't be orphaned.

## Watching it

Personal-account sign-ins to this app may not appear in the owning tenant's Entra sign-in logs at all; that is unconfirmed. The adoption dashboard tracks what is public (downloads, stars, traffic). If sign-in volume is available, alert on a sudden multiple of the normal rate. Reports of phishing that used the "Delistd" prompt arrive as issues or at privacy@drumandbytes.com.

## If it is abused or disabled

1. Register a replacement app with the settings above.
2. Set `DefaultOAuthClientID` in `internal/config/providers.go` and ship a **patch release**. Refresh tokens are bound to the app that issued them, so existing installs get "sign-in expired: run `delistd oauth login` again" and sign in again against the new app.
3. Delete or disable the old registration once the release is out. Until users update, they can switch immediately without a release: set `DELISTD_OAUTH_CLIENT_ID=<new id>` (applies to every account without its own `client_id`) or put `client_id:` on the account.
4. Say so in the release notes and on issue #293.

Users who don't want to depend on the shared app at all can register their own and set `client_id`; see [outlook.md](outlook.md).
