# Architecture

## Tech Stack

- **Language**: Go 1.26+ (module declares `go 1.26`; the toolchain auto-fetches a matching version on build if the installed `go` is older but supports toolchain switching, i.e. Go 1.21+)
- **CLI Framework**: Cobra (`github.com/spf13/cobra`)
- **Email**: SMTP only (`internal/email/smtp.go`). SendGrid and Resend were removed - see [auditing.md](auditing.md#dead-code--removed).
- **Database**: SQLite (for history tracking via `modernc.org/sqlite`)
- **Config**: YAML (`gopkg.in/yaml.v3`)
- **Browser automation**: chromedp, for `fill`/`confirm` commands and CAPTCHA detection

## Project Structure

```
eraser/
├── cmd/eraser/
│   ├── main.go                # Root cobra.Command wiring + shared helpers (config/profile/
│   │                           # broker path resolution) - each command's own flags and Run
│   │                           # logic live in their own cmd_*.go file, one per command:
│   │                           # cmd_init.go, cmd_send.go, cmd_brokers.go (list-brokers/
│   │                           # add-broker), cmd_status.go, cmd_bounces.go (mark-bounced/
│   │                           # cleanup-bounces), cmd_monitor.go, cmd_pipeline.go,
│   │                           # cmd_fill.go, cmd_confirm.go, cmd_serve.go, cmd_profile.go
├── internal/
│   ├── broker/broker.go       # Broker struct, YAML loading, filtering, add/remove
│   ├── browser/                # chromedp automation: form filling, CAPTCHA detection,
│   │                            # confirmation-link clicking, shared broker-domain allowlist
│   │                            # validation (domain.go)
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
│   └── web/
│       ├── server.go            # Server struct, NewServer, router setup, core render/helper
│       │                        # methods - handlers themselves live in handlers_*.go, grouped
│       │                        # by resource: handlers_pages.go (dashboard/brokers/history/
│       │                        # pipeline/tasks), handlers_api.go (HTMX JSON/fragment
│       │                        # endpoints), handlers_jobs.go (send-job API + background
│       │                        # send processing), handlers_settings.go, handlers_setup.go
│       │                        # (setup wizard), handlers_profile.go (profile switching)
│       ├── job.go               # Job/JobManager - background send-job state, mutex-protected
│       └── session.go           # Setup-wizard session store
├── data/brokers.yaml            # 800+ data broker database (each entry priority-tagged)
├── docs/                        # Granular reference docs (this directory)
└── EU-NOTES.md                  # GDPR/EU-specific setup and customization notes
```

CI (`.github/workflows/ci.yml`) runs `go build`/`go vet`/`go test -race` and `golangci-lint` (config: `.golangci.yml`) on every push/PR.

## Key Concepts

### Broker
Each broker in `data/brokers.yaml` (top-level key `brokers:`) has:
- `id`: Unique lowercase hyphenated identifier (e.g., `spokeo`, `been-verified`)
- `name`: Display name
- `email`: Privacy/removal contact email (may be empty string - see below)
- `website`: Company website (optional)
- `opt_out_url`: Direct opt-out link (optional)
- `region`: `us`, `eu`, or `global`
- `category`: `people-search`, `marketing`, `background-check`, `financial-b2b`, `data-intermediary`, `device-id-only` (tracks by cookie/device ID, not name/email - can't be reached via this tool's standard profile-based request), or `requires-id` (won't act on a request without a government-issued ID document or similar heavyweight identity verification - this tool won't supply one on your behalf)
- `priority`: `high`, `medium` or `low` - how much this broker matters to someone trying to get removed. Purely a filter (`--priority`, and the web UI's priority selector); it composes with `category`/`region`/status rather than replacing them, so "high priority people-search brokers" is one query. `send` and the web UI's bulk send also order high-priority brokers first, which matters when `daily_send_limit` truncates a run. See [Broker priority](#broker-priority) for how the shipped values were assigned.
- `notes`: Free-text, optional - used to record why an entry looks unusual (e.g. "use the form, not email")

#### Broker priority

The shipped `priority` values were assigned by cross-matching the database against published broker-coverage lists, then banding the result. A broker scores points for each independent source that names it, plus a bump for being a people-search site (those publish a searchable profile of you personally) or an EU broker (this fork exists for GDPR Article 17 use, so an EU-established broker is the most actionable target for its user). `high` is roughly "named by several sources, or a top-tier aggregator"; `medium` is "named by at least one"; `low` is the long tail. Any site categorised `people-search` is `high` regardless of score.

Sources used, in rough order of weight:

- Consumer Reports' *Data Defense* study (the 13 people-search sites it tested against 7 removal services)
- Optery's open-source data broker directory (CC BY-NC-SA), specifically its plan-tier field - their entry-level ("Core") coverage is the closest published proxy for "what the base plans actually include"
- PersProtect's open dataset of 499 US brokers and opt-out links (CC BY 4.0)
- The *Big-Ass Data Broker Opt-Out List* (yaelwrites)
- The Duke Sanford report's list of the largest US consumer-data aggregators, plus the credit bureaus, applied as an explicit override so the big aggregators aren't ranked by people-search popularity
- datenanfragen.de's company database (CC0) for EU credit agencies and address dealers

None of these agree with each other, which is the point of scoring across them rather than adopting any one list wholesale. To re-derive the values, re-run that cross-match and rewrite the `priority:` line of each entry; the tests in `internal/broker` assert every shipped entry carries a valid value.

**Brokers with `email: ""`** can't be reached by `send` at all - they only take requests through a web form/DSR portal, or the address bounced and was cleared by `cleanup-bounces`/`mark-bounced`. Use `eraser list-brokers --missing-email` to see them; they need manual follow-up outside the tool.

### Templates
Three email templates (`internal/template/templates/`):
- **GDPR**: Invokes EU Article 17 "Right to Erasure" - the correct default for EU users
- **CCPA**: Invokes California Consumer Privacy Act
- **Generic**: General privacy request referencing multiple laws

### Flow
1. Load user config from `~/.eraser/config.yaml`, resolve the active profile (see [multi-profile.md](multi-profile.md))
2. Load brokers from `data/brokers.yaml`
3. Filter by region and exclusions (`broker.Filter`), then by `--priority` if given (`broker.FilterByPriority`), then order high-priority first (`broker.SortByPriority`)
4. For each broker, render email template with profile + broker data
5. Send via SMTP, capped at `options.daily_send_limit` per rolling 24h window (`send` and the web UI's job sender both read this same config value - keep them in sync, see [code-patterns.md](code-patterns.md#known-quirks) for the history)
6. Record result in SQLite history, tagged with the active profile's ID; `send` skips brokers already successfully emailed in the last 25 days (per profile) so re-running is always safe
