# Commands & Configuration

## Common Commands

```bash
# Build the project
go build -o eraser ./cmd/eraser

# Run tests
go test ./...

# CLI
./eraser init                          # Interactive config setup
./eraser send [--dry-run] [--resend] [--ignore-daily-limit] [--priority high,medium]
./eraser list-brokers [--region eu] [--category financial-b2b] [--priority high] [--search kargo] [--missing-email]
./eraser status [--limit 50]
./eraser add-broker
./eraser mark-bounced <broker-id>...   # correct the record when an email actually bounced
./eraser cleanup-bounces               # find + clear bounced broker emails
./eraser monitor                       # IMAP inbox monitoring for broker replies
./eraser pipeline                      # which brokers need manual follow-up
./eraser confirm                       # click confirmation links from broker emails
./eraser fill                          # browser-automate opt-out forms
./eraser serve [-p 3000]               # web UI
./eraser profile list                  # list configured profiles
./eraser profile add                   # add a second/third named profile
```

Every command above (except `profile`, `add-broker`, `list-brokers`) accepts a global `--profile <id>` flag. It can be omitted entirely for the common single-profile setup; it's required once more than one profile is configured. See [multi-profile.md](multi-profile.md) for the full model.

## Configuration

User config is stored at `~/.eraser/config.yaml` (see `config.example.yaml` for the full schema). Key sections:

- `profile` - the legacy/primary profile: name/address/email + `additional_emails`/`name_variants`/`previous_addresses`/`additional_phones` for catching records indexed under old identities. `additional_emails` is editable from the web UI too (one address per line, commas also accepted); the other three are set via `eraser init` / `eraser profile edit` and are preserved unchanged by web-side profile edits.
- `profiles` - optional list of additional named profiles (see [multi-profile.md](multi-profile.md)); when present, this list is authoritative and `profile` above becomes vestigial unless one entry has `id: default`
- `email` - SMTP only
- `options` - `template`, `rate_limit_ms`, `daily_send_limit`, `regions`, `excluded_brokers`, `excluded_categories` (skip every broker in a category, e.g. `requires-id`, or `non-broker` to hide the search-engine/preference-service entries)

### Filtering by priority

Every broker carries a `priority` of `high`, `medium` or `low` (see [architecture.md](architecture.md#broker-priority) for how those were assigned). It's an independent axis from `category`, so the two combine and either can be left blank:

```bash
./eraser list-brokers --priority high                          # every high-priority broker
./eraser list-brokers --priority high --category people-search # both filters
./eraser list-brokers --category people-search                 # priority left blank: all priorities
./eraser send --priority high --dry-run                        # preview a high-priority-only run
```

The web UI's brokers page has the same two selectors, and "Send to All" honours whatever is selected.

There is deliberately **no** `priority` key in `config.yaml`. Priority is a per-run choice, not a standing policy, and a config key would have to be enforced separately in the CLI and web code paths (which is exactly how `excluded_categories` came to be silently ignored by the web UI once already). Use `--priority` per run instead.

Note that `send` orders high-priority brokers first within a run regardless of `--priority`. That only matters when `daily_send_limit` truncates the run: the cap then spends its budget on the brokers that matter most, and the rest go out on the next run. Nobody is dropped, and ordering within a priority band still follows `data/brokers.yaml`.
- `inbox` - IMAP settings, for `monitor`/`pipeline`/the web UI's inbox scan (shared across all profiles - see [multi-profile.md](multi-profile.md#shared-inbox))
- `pipeline` - browser automation settings for `fill`
