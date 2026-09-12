# Handling Broker Replies

What to do when a data broker answers a removal request. Most replies need no repo
change; the ones that do are broker-level facts, recorded in `data/brokers.yaml`.

## How replies reach you

Replies land in the mailbox you sent from. `eraser monitor` is the automated path:
it pulls broker-domain mail over IMAP, classifies each one
(`internal/inbox/classifier.go`), and writes SQLite history + pipeline rows
(`eraser pipeline` shows what still needs a human). Reading the inbox by hand is
the same job without the database writes.

Per-request status (sent, acknowledged, confirmed, overdue) lives in Gmail and the
SQLite history - not in this repo. Only facts about the *broker itself* go in
`data/brokers.yaml`. See [EU-NOTES.md](../EU-NOTES.md) for why there's no
review-status file here.

Once you've actioned a reply, move it out of the inbox (Trash is fine - it's
reversible; nothing here empties the trash).

## Response → action

Classifications match `ResponseType` in `internal/inbox/classifier.go`.

| Reply says | Type | Action |
|---|---|---|
| Deletion done / suppressed / "we no longer hold your data" | `success` | none - archive the mail |
| "No record / no match for you" from a genuine broker | `rejected` | none - they're still a real broker, a future re-scrape may list you again |
| "We are not a data broker" / "B2B only, not in scope" | `rejected` | remove the entry from `data/brokers.yaml`, then check the count floor (below) |
| "We won't process this by email - use our web form / portal" | `form_required` | set `email: ""`, set/refresh `opt_out_url` to the form they name, add a dated `notes:` line |
| Won't act without a government-ID document | - | set `category: requires-id`, add a dated `notes:` line (users exclude this category in bulk) |
| Confirmation / identity link to click | `confirmation_required` | click it (or run `eraser confirm`) - no data change |
| Bounce / dead address | `bounced` | `eraser mark-bounced <id>`, or set `email: ""` + dated note by hand. Never delete a real broker over a bounce - `MarkEmailUnreachable` in `internal/broker/broker.go` exists so the record survives for a later working address |
| Refuses on a real statutory exemption (FCRA / GLBA / DPPA), genuine dead end | `rejected` | set `email: ""`, add a dated `notes:` line citing the basis; keep the row (see the `goodhire` entry) |

"Remove the entry" vs. "blank the email" is a judgement call: delete when the
company is genuinely not a broker and there's nothing to come back to; blank +
note when they're a broker you just can't reach by email, or a dead end worth
remembering.

## Editing `data/brokers.yaml`

- **Dated notes**: `notes: '<summary> (YYYY-MM-DD): <detail, quoting the reply where useful>'`. Append to an existing note rather than replacing its history.
- **Commits**: one entry's worth of change per commit where practical, scoped `fix(brokers):` or `chore(brokers):`. Conventional Commits - release-please reads them.
- **Generated files**: `site/content/brokers/**` and `site/static/brokers.json` are produced by `eraser guides` and git-ignored. Don't regenerate them for a data edit.
- **Count floor**: after any add/remove, run `grep -c '^    - id:' data/brokers.yaml`. `README.md`, `docs/architecture.md`, and `EU-NOTES.md` all claim "700+" - keep the true count at or above 700 or fix those docs. `internal/broker` CI also fails below `MinSaneBrokerCount` (200).
