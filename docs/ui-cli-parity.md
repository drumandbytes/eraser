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
| Re-run the reply classifier | `monitor --reclassify` | Pipeline, Reclassify |
| Clear dead broker addresses (bounces) | `cleanup-bounces [--remove]` | Pipeline, Check bounces, Clear |
| Drafts to send by hand, in bulk | `draft -o dir` | Brokers, Download drafts (.zip) |
| Config upgraded from 1.x | message on first run | dismissible notice |
| Config edited elsewhere | read on each run | reloaded automatically while serving |

## One-sided today

| Task | Where | Why |
|---|---|---|
| `confirm` (open confirmation links in a browser) | CLI only | Needs the headless-browser automation, which the web server doesn't drive; the web UI lists confirmation links as tasks to do by hand |
| `audit-brokers`, `validate-brokers`, `guides` | CLI only | List maintenance and site generation, not end-user tasks |
| `serve`, `mcp` server | CLI only | They start the other surfaces |
