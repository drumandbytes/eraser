# Upgrading to 2.0

2.0 removes the compatibility shims that [stability.md](stability.md) kept alive through 1.x. For a normal setup there is nothing to do: the first command that loads `config.yaml` migrates it and keeps the original.

## What happens on first run

When `config.yaml` is in a 1.x shape, Eraser rewrites it once and saves the untouched original as `config.yaml.1x.bak` next to it. The backup is never overwritten by later runs. If the backup can't be written, Eraser warns, leaves the file alone and still runs with the migrated values in memory.

## What changed

| Change | Before | Now |
|---|---|---|
| Profile country | Free text (`Germany`, `UK`) | ISO 3166-1 alpha-2 code (`DE`, `GB`). The CLI prompts and the web form (now a dropdown) accept a name and store the code. A value that doesn't resolve is kept with a warning and never hides a broker |
| Legacy `profile:` block | Read and rewritten as `profiles:` on save | Migrated on first load into `profiles: [{id: default}]`; no longer read afterwards |
| `email.provider` | Accepted if `smtp`, rejected otherwise | Removed. SMTP is the only transport; a leftover `provider:` line is ignored and drops out on the next save |
| Mail passwords | Plain text in `config.yaml` | `enc:v1:` ciphertext, bound to the machine and user; the backup has them blanked |
| `eraser monitor --once` | Deprecated no-op | Removed (a single scan is the default). `eraser auto --once` is unchanged |

Mail passwords (`smtp.password`, `inbox.password`, and per-profile `mail` overrides) are no longer stored as plain text. The first load rewrites them as `enc:v1:...`, encrypted with a key derived from this machine's ID and your user, so Eraser still sends and monitors unattended. The `config.yaml.1x.bak` backup has every password blanked, so it can't leak one, which also means restoring it requires re-entering them. A config moved to another machine (or another user) loads with an empty password and a warning; re-enter it with `eraser init` or in Settings.

Emails, browser form filling, MCP profile fields and the evidence report still show the country *name*; only the stored value is a code.

## Scripts and tooling

- Anything that passed `monitor --once` must drop the flag.
- Anything that reads `config.yaml` and expects a free-text `country`, a top-level `profile:` block or `email.provider` needs updating. `eraser export --format json` fields are unchanged.
- Downgrading to 1.x afterwards is possible by restoring `config.yaml.1x.bak` and re-entering the mail passwords (the backup has them blanked); 1.x doesn't understand country codes written by 2.0 and would show them as-is.
