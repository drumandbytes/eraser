# Web UI and CLI parity

Everything a person needs should be doable from either surface. This is the state after the 2.0 work, and what is deliberately one-sided.

## Both

| Task | CLI | Web UI |
|---|---|---|
| First-time setup | `init` | Setup wizard |
| Profiles (add, edit, delete) | `profile` | Settings, Profiles |
| Country (stored as ISO code) | prompts accept a name or code | dropdown |
| Browse brokers, with country scope | `list-brokers` (hides out-of-scope; `--all-countries`) | Brokers (badges, "N hidden") |
| Send removal requests | `send` | Send, Send to All |
| Hand-send a request | `draft`, `mark-sent` | per-broker email page, Mark sent |
| Mark a bounce | `mark-bounced` | Mark bounced |
| Add / update brokers | `add-broker`, `update-brokers` | Add broker, Settings, Broker List |
| Read replies | `monitor` | Inbox scan, rescan |
| Pipeline and form tasks | `pipeline`, `fill` | Pipeline, Tasks, Forms |
| Evidence report | `export` | Export |
| Automation | `auto`, `schedule` | Settings, Automation |
| Passphrase for mail passwords | `passphrase set\|clear` | Settings, Passphrase; `/unlock` page |
| Agent batch approvals | `mcp approve` | Automation approvals |
| Config upgraded from 1.x | message on first run | dismissible notice |
| Config edited elsewhere | read on each run | reloaded automatically while serving |

## One-sided today

| Task | Where | Why / next step |
|---|---|---|
| `cleanup-bounces` (scan the inbox for bounces and clear dead broker emails) | CLI only | Web has per-broker Mark bounced only. Worth porting: needs the scan split from the printing in `cmd_bounces.go` |
| Reclassify saved replies | Web only (`/inbox/reclassify`) | Worth a CLI flag on `monitor` |
| Bulk `.eml` export (`draft -o dir`) | CLI only | Web has the single-broker page; a zip download would close it |
| `confirm` (open confirmation links in a browser) | CLI only | Web lists them as tasks to do by hand |
| `audit-brokers`, `validate-brokers`, `guides` | CLI only | List maintenance and site generation, not end-user tasks |
| `serve`, `mcp` server | CLI only | They start the other surfaces |
