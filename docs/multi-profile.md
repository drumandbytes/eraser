# Multi-Profile Support

Lets one install send/track removal requests for more than one identity (e.g. a spouse or family member) sharing the same config file, broker database, and inbox, while keeping every profile's history, pipeline status, and background send jobs fully isolated from the others.

Most installs only ever need one profile - the one set up by `eraser init`. Everything below only matters once a second profile has been added via `eraser profile add`.

## Backward compatibility

This was designed so a single-profile install needs zero changes:

- **Config**: `config.Load`/`Save` move a legacy top-level `profile:` block (pre-0.11) into `profiles:` as `id: default`, so old configs keep working and are rewritten in the current shape on the next save. `Config.GetProfiles()` still wraps an in-memory `Config.Profile` the same way.
- **History**: a new `profile_id TEXT NOT NULL DEFAULT 'default'` column was added to `removal_requests`, `broker_responses`, and `pending_tasks` via `ALTER TABLE ... ADD COLUMN ... DEFAULT`, which auto-backfills every pre-existing row to `"default"` - no manual migration step. `internal/history/history_test.go`'s `TestMigrationBackfillsExistingRowsToDefaultProfile` verifies this directly by creating a legacy pre-migration table and confirming old rows survive.

## Config model (`internal/config/config.go`)

```yaml
profiles:
  - id: default        # the primary profile, created by `eraser init` / web setup
    first_name: ...
  - id: spouse
    first_name: ...
```

- `NamedProfile` = `ID string` + inlined `Profile`
- `Config.GetProfiles() []NamedProfile` - the resolved list (see above)
- `Config.PrimaryProfile() NamedProfile` - the `default` entry, else the first; what `eraser init` edits
- `Config.GetProfile(id string) (NamedProfile, error)` - resolves one profile:
  - `id == ""` with exactly one configured profile → that profile
  - `id == ""` with more than one configured → error listing available IDs (this is what makes `--profile` "required if ambiguous")
  - non-empty `id` → case-insensitive match, or an error listing available IDs
- `Config.Validate()` iterates `GetProfiles()` and validates every one (unique case-insensitive IDs, required name/email fields)

## History scoping (`internal/history/history.go`)

Nearly every `Store` method takes a `profileID string` and filters by it. Two are deliberately **global** (no profile filter): `ClearBrokerResponses` and `GetAllBrokerResponses`. These operate on the one shared IMAP inbox that serves every profile together - a full-inbox rescan/reclassify isn't a per-profile view.

### Shared inbox

A shared mailbox carries replies for every profile's sent requests together. When processing an inbound reply, attribute it to whichever profile actually emailed that broker - not to whatever profile happens to be "active" in the CLI/web session doing the scan. That's what `Store.ResolveProfileForBroker(brokerID string) (string, error)` is for: it looks up the most recent `removal_requests` row for that broker across *all* profiles and returns its `profile_id` (falling back to `"default"` if the broker was never emailed by anyone). `inbox.RecordReply` (used by `Monitor.ScanAndStore` for the CLI monitor, the web scan/rescan and automated cycles) calls this per email before storing the classified response.

## Per-profile email accounts (`internal/config/config.go`)

By default every profile shares the top-level `email:` (SMTP, for sending)
and `inbox:` (IMAP, for monitoring replies) blocks. A profile can instead set
its own `mail:` block to send and get its replies monitored through a
dedicated account - e.g. so each household member's requests don't all pile
onto one address, lowering per-broker send limits and making it harder to
tell whose reply is whose ([#75](https://github.com/drumandbytes/eraser/issues/75)):

```yaml
profiles:
  - id: spouse
    first_name: ...
    mail:
      email:              # overrides the shared email: block for this profile's sends
        from: spouse@example.org
        smtp:
          host: smtp.fastmail.com
          port: 465
          username: spouse@example.org
          password: app-password
      inbox:               # overrides the shared inbox: block for this profile's replies
        enabled: true
        provider: fastmail # preset id fills server/port; or set server + port
        email: spouse@example.org
        password: app-password
```

Either field can be set independently - a profile can override just `email`
(send from its own account, still share the default inbox for replies) or
just `inbox`. Send-only providers (Amazon SES) get only `email`, so their
replies fall back to the shared inbox.

- `Config.EmailForProfile(p NamedProfile) EmailConfig` / `Config.InboxForProfile(p NamedProfile) InboxConfig` - resolve the effective config for one profile (its `mail.email`/`mail.inbox` override if set, otherwise the shared top-level block). Every send call site (CLI `send`, the web UI's send-one/send-all/resume-job paths) goes through `EmailForProfile` instead of reading `Config.Email` directly.
- `Config.ConfiguredInboxes() []InboxConfig` - every distinct enabled inbox across all profiles, deduplicated by email address. `eraser monitor` scans each one in turn (concurrently, only under `--watch`, since each watch blocks); the web UI's inbox scan/rescan/reclassify only cover the active profile's own inbox (`InboxForProfile`) per request - run `eraser monitor` for full multi-inbox coverage.
- `eraser profile add`/`profile edit` (CLI `promptMailAccount`) and the web UI's add/edit profile forms (`partials/mail-account.html`, `internal/web/mailform.go`) set/change/remove a profile's `mail` override with the same provider presets as setup (`config.Providers`).
- `Config.Validate()` validates a profile's `mail.email` override with the same rules as the shared `email:` block.

## CLI (`cmd/eraser/`)

- Global persistent `--profile <id>` flag, resolved via `resolveProfile(cfg)` → `cfg.GetProfile(profileFlag)`
- Threaded through: `send`, `status`, `monitor`, `pipeline`, `fill`, `confirm`, `mark-bounced`, `export`, `draft`, `mark-sent`
- `eraser profile list` - prints configured profiles
- `eraser profile add` - interactively appends a new `NamedProfile` to `config.Profiles`
- `eraser init`, when re-run on an existing config, preserves `existing.Profiles` (previously it silently dropped anything added via `profile add`); if a `"default"` entry exists among them, it's kept in sync with whatever `profile:` fields were just re-entered

## Web UI (`internal/web/`, `templates/layout.html`)

- Active profile is tracked via a plain (non-secret) cookie, `eraser_profile` - **not** the `SessionStore` in `internal/web/session.go`, which is purpose-built for the ephemeral setup-wizard flow only
- `Server.activeProfile(r *http.Request) config.NamedProfile` (in `server.go`) reads/validates the cookie against `config.GetProfiles()`, falling back to the first configured profile
- `POST /api/profile` (`handleAPISwitchProfile`, in `handlers_profile.go`) validates `profile_id`, sets the cookie, and redirects to a same-origin `redirect` form value
- `renderWithCSRF` injects `Profiles`/`ActiveProfile`/`CurrentPath` into every page's template data, so `layout.html`'s nav can render the switcher unconditionally without every handler wiring it manually
- The switcher itself is a `<select>` inside a small auto-submitting `<form>` (desktop nav + mobile nav), only rendered when `len(.Profiles) > 1`
- Background send jobs (`internal/web/job.go`) carry `ProfileID`; `JobManager.GetActive(profileID)` and `.Create(total, profileID)` are profile-scoped, so two profiles can have a send running concurrently without colliding on "job already active"

## Adding a new per-profile data path

If you add a new `history.Store` method or web/CLI code path that reads or writes user-specific data, give it a `profileID` parameter and filter by it - follow the pattern of the existing methods rather than introducing a new scoping mechanism. If the data instead comes from the shared inbox (like broker replies), resolve the owning profile via `ResolveProfileForBroker` rather than using "whatever profile is active right now".
