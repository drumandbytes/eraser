# Commands & Configuration

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
./eraser draft [<broker-id>...] [--region eu] [--category people-search] [-o ./out]  # render emails to send by hand
./eraser mark-sent <broker-id>... [--region eu] [--category ...] [--dry-run]         # record a hand-sent request
./eraser send --manual                 # walk the list, mark each one as you send it yourself
./eraser add-broker
./eraser mark-bounced <broker-id>...   # correct the record when an email actually bounced
./eraser cleanup-bounces               # find + clear bounced broker emails
./eraser audit-brokers [--region eu] [--category financial-b2b] [--timeout 15]  # MX/website liveness check
./eraser monitor                       # IMAP inbox monitoring for broker replies
./eraser pipeline                      # which brokers need manual follow-up
./eraser export [-o file] [--format html|json] [--since 2026-01-01]  # evidence report for a DPA/noyb complaint
./eraser confirm                       # click confirmation links from broker emails
./eraser fill                          # browser-automate opt-out forms
./eraser serve [-p 3000]               # web UI
./eraser profile list                  # list configured profiles
./eraser profile add                   # add a second/third named profile
```

Every command above (except `profile`, `add-broker`, `list-brokers`) accepts a global `--profile <id>` flag. It can be omitted entirely for the common single-profile setup; it's required once more than one profile is configured. See [multi-profile.md](multi-profile.md) for the full model.

## Configuration

User config is stored at `~/.eraser/config.yaml` (see `config.example.yaml` for the full schema). Key sections:

- `profile` - the legacy/primary profile: name/address/email + `additional_emails`/`name_variants`/`previous_addresses`/`additional_phones` for catching records indexed under old identities
- `profiles` - optional list of additional named profiles (see [multi-profile.md](multi-profile.md)); when present, this list is authoritative and `profile` above becomes vestigial unless one entry has `id: default`
- `email` - SMTP only
- `options` - `template`, `rate_limit_ms`, `daily_send_limit`, `regions`, `excluded_brokers`, `excluded_categories` (skip every broker in a category, e.g. `requires-id`), `send_mode` (`manual` = Eraser never sends; render with `draft` / `send --manual`, record with `mark-sent`; no `email:` block needed)
- `inbox` - IMAP settings, for `monitor`/`pipeline`/the web UI's inbox scan (shared across all profiles - see [multi-profile.md](multi-profile.md#shared-inbox))
- `pipeline` - browser automation settings for `fill`
