# Stability and versioning

Eraser follows [semantic versioning](https://semver.org/) from 1.0.0 onward. A change that could break a working setup needs a major version (2.0). New features come in minor versions (1.x), and fixes and broker-list updates come in patch versions.

This page defines what counts as "a working setup".

## Stable: won't break within 1.x

- **`config.yaml`**: every key in the schema in [commands.md](commands.md#configuration). A config that works on 1.0 keeps loading and behaving the same on every 1.x. New keys are optional and default to the old behaviour. Older formats still load and are rewritten in the current shape on the next save:
  - the pre-0.11 top-level `profile:` block becomes `profiles: [{id: default}]`
  - `email.provider: smtp` is accepted
  - `smtp.use_tls` can be set explicitly
  - removed keys such as `pipeline.auto_confirm` are ignored
- **CLI commands and flags** shown by `eraser --help` and `eraser <command> --help`:
  - Names, arguments and meaning stay the same.
  - Exit codes: 0 means success and non-zero means failure. Two flags use them deliberately: `update-brokers --check` exits 1 when an update is available, and `audit-brokers --fail-on-dead` exits non-zero when a broker is dead.
  - A flag that's retired keeps working, with a deprecation warning, until 2.0. Example: `monitor --once`, which is now the default behaviour.
- **`history.db`**: upgrades migrate automatically on first run with no manual step. Downgrading to an older version after an upgrade isn't supported.
- **`brokers.yaml` format** (`id`, `name`, `email`, `website`, `opt_out_url`, `region`, `category`, `notes`): `update-brokers` on any 1.x release fetches the current list from `main`, so the format may only gain optional fields. Your own `options.broker_file` lists stay valid.
- **`eraser export --format json`** field names: fields may be added, never renamed or removed. Tools that process the evidence report can rely on them.

## Not covered: may change in any release

- The web UI: its pages, routes, HTMX endpoints and layout. It's a local interface, not an API.
- Human-readable CLI output: tables, emoji and wording. Use `export --format json` for anything machine-read.
- The removal-request templates' wording. It's improved as broker replies teach us what works.
- The contents of the broker lists and of the mail-provider presets in setup. Presets only prefill fields, so a changed preset never alters a saved config.
- Hidden commands, such as `guides`, the project website's page generator.
- Go packages under `internal/`. Eraser is a program, not a library.

## Before you upgrade

Release notes in [CHANGELOG.md](../CHANGELOG.md) list every change. A major version's notes include the steps for any manual migration.
