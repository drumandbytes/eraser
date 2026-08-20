# Architecture

## Tech Stack

- **Language**: Go 1.25+ (module declares `go 1.25.0`; the toolchain auto-fetches a matching version on build if the installed `go` is older but supports toolchain switching, i.e. Go 1.21+)
- **CLI Framework**: Cobra (`github.com/spf13/cobra`)
- **Email**: SMTP only (`internal/email/smtp.go`). SendGrid and Resend were removed - see [auditing.md](auditing.md#dead-code--removed).
- **Database**: SQLite (for history tracking via `modernc.org/sqlite`)
- **Config**: YAML (`gopkg.in/yaml.v3`)
- **Browser automation**: chromedp, for `fill`/`confirm` commands and CAPTCHA detection

## Project Structure

```
eraser/
├── cmd/eraser/main.go        # CLI entry point, all commands (init/send/list-brokers/status/
│                              # add-broker/mark-bounced/cleanup-bounces/monitor/pipeline/
│                              # fill/confirm/serve/profile)
├── internal/
│   ├── broker/broker.go       # Broker struct, YAML loading, filtering, add/remove
│   ├── browser/                # chromedp automation: form filling, CAPTCHA detection,
│   │                            # confirmation-link clicking
│   ├── config/config.go        # User configuration (profile(s), email, options, inbox, pipeline)
│   ├── email/
│   │   ├── sender.go            # Sender interface + NewSender (SMTP only)
│   │   └── smtp.go              # SMTP implementation
│   ├── history/history.go      # SQLite history tracking, pipeline status, per-profile scoping
│   ├── inbox/                   # IMAP monitoring + reply classification (success/form-required/
│   │                             # confirmation/rejection/pending/bounced)
│   ├── template/
│   │   ├── template.go          # Template rendering engine
│   │   └── templates/           # Embedded: gdpr.tmpl, ccpa.tmpl, generic.tmpl
│   └── web/                     # Web UI: chi router, HTMX partials, setup wizard, job manager
├── data/brokers.yaml            # 774+ data broker database
├── docs/                        # Granular reference docs (this directory)
└── EU-NOTES.md                  # GDPR/EU-specific setup and customization notes
```

There is no `.github/workflows/` in this fork - the original README's "Automate with GitHub Actions" section described a feature that was never actually present here and has been removed from the docs.

## Key Concepts

### Broker
Each broker in `data/brokers.yaml` (top-level key `brokers:`) has:
- `id`: Unique lowercase hyphenated identifier (e.g., `spokeo`, `been-verified`)
- `name`: Display name
- `email`: Privacy/removal contact email (may be empty string - see below)
- `website`: Company website (optional)
- `opt_out_url`: Direct opt-out link (optional)
- `region`: `us`, `eu`, or `global`
- `category`: `people-search`, `marketing`, `background-check`, `financial-b2b`, or `data-intermediary`
- `notes`: Free-text, optional - used to record why an entry looks unusual (e.g. "use the form, not email")

**Brokers with `email: ""`** can't be reached by `send` at all - they only take requests through a web form/DSR portal, or the address bounced and was cleared by `cleanup-bounces`/`mark-bounced`. Use `eraser list-brokers --missing-email` to see them; they need manual follow-up outside the tool.

### Templates
Three email templates (`internal/template/templates/`):
- **GDPR**: Invokes EU Article 17 "Right to Erasure" - the correct default for EU users
- **CCPA**: Invokes California Consumer Privacy Act
- **Generic**: General privacy request referencing multiple laws

### Flow
1. Load user config from `~/.eraser/config.yaml`, resolve the active profile (see [multi-profile.md](multi-profile.md))
2. Load brokers from `data/brokers.yaml`
3. Filter by region and exclusions (`broker.Filter`)
4. For each broker, render email template with profile + broker data
5. Send via SMTP, capped at `options.daily_send_limit` per rolling 24h window (`send` and the web UI's job sender both read this same config value - keep them in sync, see [code-patterns.md](code-patterns.md#known-quirks) for the history)
6. Record result in SQLite history, tagged with the active profile's ID; `send` skips brokers already successfully emailed in the last 25 days (per profile) so re-running is always safe
