# Commands & Configuration

## Common Commands

```bash
# Build the project
go build -o eraser ./cmd/eraser

# Run tests
go test ./...

# CLI
./eraser init                          # Interactive config setup
./eraser send [--dry-run] [--resend] [--ignore-daily-limit] [--list full|verified] [--broker id] [--region eu] [--category financial-b2b] [--exclude id] [--status eligible|never|failed|all]
./eraser list-brokers [--region eu,global] [--category financial-b2b] [--search kargo] [--missing-email]
./eraser status [--limit 50]
./eraser draft [<broker-id>...] [--region eu] [--category people-search] [-o ./out]  # render emails to send by hand
./eraser mark-sent <broker-id>... [--region eu] [--category ...] [--dry-run]         # record a hand-sent request
./eraser send --manual                 # walk the list, mark each one as you send it yourself
./eraser add-broker
./eraser mark-bounced <broker-id>...   # correct the record when an email actually bounced
./eraser cleanup-bounces               # find + clear bounced broker emails
./eraser audit-brokers [--region eu] [--category financial-b2b] [--timeout 15] [--fail-on-dead] [--fix]  # MX/website liveness check; --fix clears dead email domains
./eraser validate-brokers [file]      # structural check: ids, names, regions, emails, URLs (exit non-zero on any problem); no arg checks both built-in lists
./eraser update-brokers [--check]      # fetch the latest broker list (conditional; writes ~/.eraser/brokers.yaml)
./eraser guides [-o site/content] [--format md|html]  # generate opt-out guide pages + a JSON broker directory
./eraser monitor                       # IMAP inbox monitoring for broker replies
./eraser auto [--once] [--every 6h]    # unattended cycle: send what's due (all profiles), then scan inboxes
./eraser schedule install|remove|status  # have launchd/systemd run 'auto --once' every 6 hours
./eraser pipeline                      # which brokers need manual follow-up
./eraser export [-o file] [--format html|json] [--since 2026-01-01]  # evidence report for a DPA/noyb complaint
./eraser confirm                       # click confirmation links from broker emails
./eraser fill                          # browser-automate opt-out forms
./eraser serve [-p 3000]               # web UI
./eraser profile list                  # list configured profiles
./eraser profile add                   # add a second/third named profile
```

The broker list is embedded in the binary. For the send-family commands (`send`, `draft`, `mark-sent`, `serve`) the resolution order is: `--brokers <path>` flag → `options.broker_file` → `options.broker_list: verified` (the smaller registry-sourced list, `data/brokers-verified.yaml`) → `~/.eraser/brokers.yaml` (written by `update-brokers`) → the embedded full list. `send --list full|verified` overrides `options.broker_list` per run. `--region` and `--category` are repeatable or comma-separated on `send`, `draft`, `mark-sent`, `list-brokers` and `audit-brokers` (values OR together within a flag, AND across flags); `send` also takes `--broker` and `--exclude` the same way. `--status eligible` is the safe default (never sent, failed, or last success at least 25 days old); `never`, `failed`, and `all` are explicit alternatives. Other commands (`audit-brokers`, `guides`, `list-brokers`, reply processing) always act on the full list. `add-broker` and `cleanup-bounces` write to `./data/brokers.yaml` in a source checkout, or `~/.eraser/brokers.yaml` otherwise.

`auto` runs one cycle per call with `--once`, or loops every `--every` (min 1h) in the foreground. `schedule install` writes a launchd agent (`~/Library/LaunchAgents/com.drumandbytes.eraser.auto.plist`, output in `auto.log` next to the config) or a systemd user timer (`eraser-auto.timer`, output in the journal) that runs `auto --once --config <abs path>` at 00/06/12/18:07; missed slots run on wake. Every 6 hours rather than daily because `daily_send_limit` is a rolling 24h window: a run exactly 24h after the last one would find the cap still used up. All modes share `auto.lock` in the config directory, so cycles never overlap, and write the last result to `auto-state.json` (shown by `schedule status`). The loop refuses to start while the OS job is installed. With `send_mode: manual`, cycles only scan the inbox.

The web UI's Settings → Automation card does the same without a terminal: install/remove the OS job, turn on the in-app scheduler (`schedule.enabled`: `serve` runs `eraser auto --once` as a child process every 6 hours while it's open), or run a cycle now. It covers every profile, not just the active one. "Send all" refuses while a cycle holds the lock, and a daily-cap-paused job resumed at startup skips brokers sent since it paused.

Every command above (except `profile`, `add-broker`, `list-brokers`) accepts a global `--profile <id>` flag. It can be omitted entirely for the common single-profile setup; it's required once more than one profile is configured. See [multi-profile.md](multi-profile.md) for the full model.

Web UI equivalents for the per-profile commands: `export` → History → Export evidence (HTML or JSON); `mark-bounced` → the "bounced?" link next to a Sent status on Brokers; `update-brokers` → Settings → Broker List (also reloads the running server's list). Broker-list maintenance (`add-broker`, `audit-brokers`, `validate-brokers`, `cleanup-bounces`, `guides`) and browser automation (`fill`, `confirm`) stay CLI-only.

## Configuration

Which of these keys and commands are covered by the 1.x compatibility promise: [stability.md](stability.md).

User config is stored at `~/.eraser/config.yaml` (see `config.example.yaml` for the full schema). Key sections:

- `profiles` - one entry per person: `id` (stable, stored in history - `default` for the one `eraser init` creates) + name/address/email + `additional_emails`/`name_variants`/`previous_addresses`/`additional_phones` for catching records indexed under old identities (editable in `eraser init` / `eraser profile edit` and on the web profile form, one per line). Each entry can set its own `mail.email`/`mail.inbox` to send/monitor through a dedicated account instead of the shared blocks below - see [multi-profile.md](multi-profile.md). A pre-0.11 top-level `profile:` block still loads and is rewritten as `profiles: [{id: default, ...}]` on the next save
- `email` - `from` + `smtp` (`host`, `port`, `username`, `password`). TLS is on by default: port 465 = implicit TLS, any other port = mandatory STARTTLS; `use_tls: false` is only accepted without a username (a local relay). Loopback hosts (Proton Mail Bridge) skip certificate verification. `provider:` is optional (only `smtp` is accepted). Setup presets live in `internal/config/providers.go`
- `options` - `template`, `rate_limit_ms`, `daily_send_limit`, `broker_list` (`full`/`verified`), `broker_file` (path to your own list), `regions`, `excluded_brokers`, `excluded_categories` (skip every broker in a category, e.g. `requires-id`), `send_mode` (`manual` = Eraser never sends; render with `draft` / `send --manual`, record with `mark-sent`; no `email:` block needed)
- `inbox` - IMAP settings (`provider` = a preset id that fills `server`/`port`, or set them directly; 993 = TLS, other ports = STARTTLS), for `monitor`/`pipeline`/the web UI's inbox scan. Shared by default across every profile that doesn't set its own `mail.inbox` override (see [multi-profile.md](multi-profile.md#shared-inbox)); `monitor` scans every distinct inbox in one run, the web UI's scan/rescan only the active profile's own
- `pipeline` - browser automation settings for `fill`
- `schedule` - `enabled: true` makes a running `eraser serve` run an automated cycle every 6 hours (the in-app fallback when the OS job from `schedule install` isn't set up; ignored while it is). Set from the web UI's Settings → Automation card too
