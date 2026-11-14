# Outlook.com, Hotmail and Live

Microsoft no longer accepts passwords or app passwords for IMAP and SMTP on personal accounts ([Microsoft's settings page](https://support.microsoft.com/en-us/outlook/pop-imap-and-smtp-settings-for-outlook-com) says Outlook.com requires OAuth2). Delistd signs in the way Microsoft requires, with a short code you enter in a browser.

This is new and only tested against a fake Microsoft server so far. If it works for you, or doesn't, please say so in [issue #293](https://github.com/drumandbytes/delistd/issues/293).

It is for **personal** accounts (outlook.com, hotmail.com, live.com). Microsoft 365 work and school accounts are not covered. If you don't want to set this up, the simpler route is a separate Gmail or other app-password mailbox just for Delistd; see [getting-started.md](getting-started.md).

## 1. Register a free Microsoft app (once)

Microsoft only allows sign-in through a registered app, and Delistd ships none, so you register your own. It has no secret and costs nothing.

1. Open [entra.microsoft.com](https://entra.microsoft.com) → **App registrations** → **New registration**, signed in with your Microsoft account.
2. Name: anything (e.g. `Delistd`). Supported account types: **Personal Microsoft accounts only**. Leave the redirect URI empty. **Register**.
3. **Authentication** → **Advanced settings** → **Allow public client flows** → **Yes** → **Save**.
4. **API permissions**: if the sign-in in step 3 below complains that the app lacks permission, add the delegated permissions `IMAP.AccessAsUser.All` and `SMTP.Send` (under *Office 365 Exchange Online*) and `offline_access`.
5. From **Overview**, copy the **Application (client) ID**.

**This step can fail for a personal account.** Microsoft gives a personal account no directory of its own, and some accounts are refused at the Entra sign-in itself (one tester got `AADSTS90123: access_denied`). If that happens, the ways in are a free Azure account (Microsoft may ask for a phone number and a payment method to verify it) or a free [Microsoft 365 Developer Program](https://developer.microsoft.com/microsoft-365/dev-program) tenant, registering the app there with the account type set to personal accounts. If neither is acceptable, use a separate Gmail or other app-password mailbox instead. We are looking at removing this step.

## 2. Sign in

**In the web UI:** pick **Outlook.com / Hotmail / Live (OAuth)** as the provider, in the setup wizard's email step, on Settings → Inbox, or in a profile's own mail account. The form shows these steps, asks for the client ID, and has a **Sign in with Microsoft** button: it shows a code and a link, you approve it at Microsoft, and the form then saves like any other account.

**From the command line:** run `delistd init` first if you haven't (any provider; the next step replaces the mail account). Then:

```bash
delistd oauth login --client-id <your client id> --email you@outlook.com
```

Delistd prints a link and a code. Open the link on any device, enter the code, and approve. Delistd then stores the sign-in, encrypted like a password, as both your sending account and your inbox, with `smtp-mail.outlook.com:587` and `outlook.office365.com:993`. It writes `auth: oauth2` and your `client_id` next to them in `config.yaml`.

Check it works with `delistd monitor` (signs in to the inbox) and a test send from the web UI's Test Your Setup step or `delistd send` for one broker.

## What to expect

- The sign-in is refreshed automatically on every run and saved back to the config. It stops working if you don't use it for about 90 days, change your Microsoft password, or revoke it; Delistd then says to run `delistd oauth login` again.
- Unattended runs (`delistd schedule install`, `delistd auto`) work, with the usual limit that a passphrase-protected config can't be unlocked by the OS scheduler.
- The command line works in Docker too, if you prefer it: `docker exec -it <container> delistd oauth login --client-id ...`.
- The refresh token never reaches your browser: the form only carries a one-time id for the finished sign-in.
- If sending fails with an authentication error while the inbox works, Microsoft may be refusing SMTP for the account. Tell us in the issue above with the exact error; this part is the least verified.
