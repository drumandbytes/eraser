# CLAUDE.md

## Project Overview

Eraser is a CLI + web tool that sends data removal requests to data brokers. This is a maintained fork of [digisamroc/eraser](https://github.com/digisamroc/eraser) (inactive since early 2026), hosted at `drumandbytes/eraser`, customized for a single EU (Latvia) user exercising GDPR Article 17 rights rather than the original US-only CCPA use case. See [EU-NOTES.md](EU-NOTES.md) for the GDPR-specific setup and broker notes, and [README.md](README.md) for user-facing docs.

## Tech Stack

- **Language**: Go 1.25+ (module declares `go 1.25.0`; the toolchain auto-fetches a matching version on build if the installed `go` is older but supports toolchain switching, i.e. Go 1.21+)
- **CLI Framework**: Cobra (`github.com/spf13/cobra`)
- **Email**: SMTP only (`internal/email/smtp.go`). SendGrid and Resend were removed - see "Dead Code / Removed" below.
- **Database**: SQLite (for history tracking via `modernc.org/sqlite`)
- **Config**: YAML (`gopkg.in/yaml.v3`)
- **Browser automation**: chromedp, for `fill`/`confirm` commands and CAPTCHA detection

## Project Structure

```
eraser/
├── cmd/eraser/main.go        # CLI entry point, all commands (init/send/list-brokers/status/
│                              # add-broker/mark-bounced/cleanup-bounces/monitor/pipeline/
│                              # fill/confirm/serve)
├── internal/
│   ├── broker/broker.go       # Broker struct, YAML loading, filtering, add/remove
│   ├── browser/                # chromedp automation: form filling, CAPTCHA detection,
│   │                            # confirmation-link clicking
│   ├── config/config.go        # User configuration (profile, email, options, inbox, pipeline)
│   ├── email/
│   │   ├── sender.go            # Sender interface + NewSender (SMTP only)
│   │   └── smtp.go              # SMTP implementation
│   ├── history/history.go      # SQLite history tracking, pipeline status
│   ├── inbox/                   # IMAP monitoring + reply classification (success/form-required/
│   │                             # confirmation/rejection/pending/bounced)
│   ├── template/
│   │   ├── template.go          # Template rendering engine
│   │   └── templates/           # Embedded: gdpr.tmpl, ccpa.tmpl, generic.tmpl
│   └── web/                     # Web UI: chi router, HTMX partials, setup wizard, job manager
├── data/brokers.yaml            # 777+ data broker database
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

**Brokers with `email: ""`** (48 of them as of this writing) can't be reached by `send` at all - they only take requests through a web form/DSR portal, or the address bounced and was cleared by `cleanup-bounces`/`mark-bounced`. Use `eraser list-brokers --missing-email` to see them; they need manual follow-up outside the tool.

### Templates
Three email templates (`internal/template/templates/`):
- **GDPR**: Invokes EU Article 17 "Right to Erasure" - the correct default for EU users
- **CCPA**: Invokes California Consumer Privacy Act
- **Generic**: General privacy request referencing multiple laws

### Flow
1. Load user config from `~/.eraser/config.yaml`
2. Load brokers from `data/brokers.yaml`
3. Filter by region and exclusions (`broker.Filter`)
4. For each broker, render email template with user + broker data
5. Send via SMTP, capped at `options.daily_send_limit` per rolling 24h window (`send` and the web UI's job sender both read this same config value - keep them in sync, see "Known quirks" below)
6. Record result in SQLite history; `send` skips brokers already successfully emailed in the last 25 days so re-running is always safe

## Common Commands

```bash
# Build the project
go build -o eraser ./cmd/eraser

# Run tests
go test ./...

# CLI
./eraser init                          # Interactive config setup
./eraser send [--dry-run] [--resend] [--ignore-daily-limit]
./eraser list-brokers [--region eu] [--category financial-b2b] [--search kargo] [--missing-email]
./eraser status [--limit 50]
./eraser add-broker
./eraser mark-bounced <broker-id>...   # correct the record when an email actually bounced
./eraser cleanup-bounces               # find + clear bounced broker emails
./eraser monitor                       # IMAP inbox monitoring for broker replies
./eraser pipeline                      # which brokers need manual follow-up
./eraser confirm                       # click confirmation links from broker emails
./eraser fill                          # browser-automate opt-out forms
./eraser serve [-p 3000]               # web UI
```

## Configuration

User config is stored at `~/.eraser/config.yaml` (see `config.example.yaml` for the full schema). Key sections: `profile` (name/address/email + `additional_emails`/`name_variants`/`previous_addresses`/`additional_phones` for catching records indexed under old identities), `email` (SMTP only), `options` (template, `rate_limit_ms`, `daily_send_limit`, `regions`, `excluded_brokers`), `inbox` (IMAP, for `monitor`), `pipeline` (browser automation settings for `fill`).

## Code Patterns

- **Error handling**: Wrap errors with context using `fmt.Errorf("context: %w", err)`. Never pass a variable directly as a format string to `fmt.Errorf`/`fmt.Sprintf` (e.g. `fmt.Errorf(someVar)`) - `go vet` flags this, and it's a real bug if the variable can ever contain a `%`.
- **Config loading**: YAML with struct tags for marshaling; defaults applied in `config.Load`.
- **Templates**: Go `text/template` with embedded files via `//go:embed`.
- **CLI commands**: Defined in `cmd/eraser/main.go` using Cobra; flags declared in the `*Cmd()` constructor, business logic in a separate `run*()` function so it's testable/callable independent of Cobra.
- **Regexes**: Compile once at package level (`var fooRe = regexp.MustCompile(...)`), never inside a function that runs per-email/per-loop-iteration. `internal/inbox/classifier.go` and `parser.go` follow this.
- **SQLite**: `internal/history/history.go` has a composite index `(broker_id, sent_at DESC)` for the "most recent record per broker" query pattern used throughout.

## Auditing this repo

This codebase gets periodic dead-code/security sweeps. To repeat one:

```bash
go build ./... && go vet ./... && go test ./...
go run honnef.co/go/tools/cmd/staticcheck@latest ./...
go run golang.org/x/tools/cmd/deadcode@latest ./...      # unreachable-from-main functions
go run golang.org/x/vuln/cmd/govulncheck@latest ./...    # known CVEs in reachable code paths
```

`deadcode` and `staticcheck` report false positives for anything only reachable via Go's `text/template` reflection (a template calling `{{.SomeMethod}}`) - grep the `.html` templates for `{{[^}]*SomeSymbol[^}]*}}` before deleting something it flags, not just a plain substring match (e.g. "Country" contains "Count").

**Known quirks worth knowing before touching related code:**
- `internal/web/server.go`'s `processSendJob` (the web UI's background sender) and `cmd/eraser/main.go`'s `send` command both need to respect `config.Options.DailySendLimit` - they used to disagree (the web UI had its own hardcoded, provider-based limit left over from the SendGrid/Resend era). Fixed, but if you add a third send path, wire it the same way.
- The web server binds to `127.0.0.1` only (`internal/web/server.go`, `NewServer`/`Start`) - keep it that way. `gorilla/csrf` has an unpatched CSRF-bypass CVE (GO-2025-3884, no fix available upstream as of this writing); loopback-only binding is the mitigation, not the CSRF middleware itself.
- `internal/web/job.go`'s `JobManager` runs its own `Cleanup` on an hourly ticker (started in `NewJobManager`) to evict completed jobs after `jobRetention` (7 days) - if you see completed-job data missing after a week, that's why, not a bug.

## Dead Code / Removed

Removed in the most recent audit (all confirmed zero-caller via `deadcode` + manual grep before deletion):
- `broker.LoadFromDir` (unused directory-loader variant of `LoadFromFile`)
- `browser.Browser.{GetPageHTML,GetPageTitle,WaitForNavigation,EnablePageEvents}` (unused chromedp wrappers)
- `browser.DetectCaptchaFromHTML` (static-HTML CAPTCHA detection; the live `detectCaptcha` via chromedp is what's actually used)
- `browser.FormFiller.{FillDropdown,CheckCheckbox,DetectFormFields}` and the `FormField` struct (unused form-filling helpers beyond what `Fill()` actually calls)
- `history.Store.GetLastRequestForBroker`
- `inbox.{ClassifyBatch,FilterByType,GetActionableResponses,GetBouncedResponses}`
- `inbox.Monitor.MoveToFolder`
- `inbox.ExtractConfirmationToken`
- `template.Engine.AvailableTemplates`
- `web.SessionStore.Count` (no metrics/debug endpoint ever called it)

Also removed: the `github.com/sendgrid/sendgrid-go` and `github.com/resend/resend-go/v2` dependencies (dead - `email.NewSender` has only ever supported `smtp`, and the setup wizard never offered those providers as options) and the `stretchr/testify` indirect dependency (was only pulled in transitively by the packages above). `go.mod`/`go.sum` cleaned via `go mod tidy`.

If you're re-adding one of these, check git history for the implementation rather than rewriting from scratch - they weren't broken, just unreferenced.
