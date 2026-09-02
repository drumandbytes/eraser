# Auditing This Repo

This codebase gets periodic dead-code/security sweeps. To repeat one:

```bash
go build ./... && go vet ./... && go test -race ./...
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest run --max-issues-per-linter=0 --max-same-issues=0 ./...
go run honnef.co/go/tools/cmd/staticcheck@latest ./...
go run golang.org/x/tools/cmd/deadcode@latest ./...      # unreachable-from-main functions
go run golang.org/x/vuln/cmd/govulncheck@latest ./...    # known CVEs in reachable code paths
```

`golangci-lint` (config: `.golangci.yml` - errcheck, govet, staticcheck, unused) also runs in CI on every push/PR via `.github/workflows/ci.yml`, so a plain `go build`/`go vet`/`go test` pass locally isn't the whole picture - run the lint command above too before assuming a change is clean. The default `max-same-issues`/`max-issues-per-linter` caps hide duplicates past the first few, which reads as "mostly clean" when it isn't - always pass `--max-issues-per-linter=0 --max-same-issues=0` when actually auditing, not just spot-checking.

## Growing the broker list from state registries

The US state data-broker registries are public and worth mining for entries the
list is missing. Each is a one-click download, not an API:

- **California** (CPPA, DELETE Act) - https://cppa.ca.gov/data_broker_registry/ - the largest, ~500+ brokers, CSV export
- **Vermont** - https://sos.vermont.gov/data-brokers/ (Secretary of State) - ~120, CSV/searchable
- **Oregon** - https://sos.oregon.gov/business/pages/data-brokers.aspx - 2024 registry
- **Texas** - https://comptroller.texas.gov/programs/data-broker/ - 2024 registry

Download a registry as CSV, then:

```bash
go run ./scripts/import-registries -csv registry.csv \
    -name-col "Data Broker Name" -url-col "Website" -email-col "Email Address"
```

It diffs the rows against the embedded list and writes `candidates.yaml` (likely
new, in `brokers.yaml` shape) and `review.md` (fuzzy matches to eyeball). Fill in
`category` and a working `email`/`opt_out_url` for the candidates you keep, move
them into `data/brokers.yaml`, then run `eraser audit-brokers` to drop any that
are already dead. Both output files are gitignored.

## Security/Correctness Sweep (2026-08)

A focused review of `internal/browser`, `internal/web`, `internal/history`, and `internal/inbox` (the packages that had zero test coverage and handle either real user PII, untrusted email content, or concurrent web-server state) turned up and fixed 15 findings - see the corresponding commits for detail:

- PII sent to unvalidated domains before autofilling opt-out forms, and confirmation-link redirects not re-validated hop-to-hop (both closed via `internal/browser/domain.go`'s shared allowlist check)
- a `Server.config` data race in the web UI (fixed via `atomic.Pointer`)
- cross-profile data access in `internal/history`'s by-ID task/response methods (now enforce `profile_id`)
- unbounded memory reads on untrusted email/attachment content in `internal/inbox` (now capped)
- several lower-severity issues: dead form-submit selectors, fill errors silently reported as success, job-state races, IMAP mailbox over-expunge, ignored context cancellation on IMAP calls, unscoped job status/cancel endpoints, `<select>`-field fill bugs, uncapped request bodies, swallowed migration errors, and an SSRF gap on private/localhost URLs

`internal/browser`, `internal/web`, and `internal/inbox` also went from zero tests to targeted coverage of these specific fixes (not full coverage) in the same effort - see each package's `*_test.go` files.

`deadcode` and `staticcheck` report false positives for anything only reachable via Go's `text/template` reflection (a template calling `{{.SomeMethod}}`) - grep the `.html` templates for `{{[^}]*SomeSymbol[^}]*}}` before deleting something it flags, not just a plain substring match (e.g. "Country" contains "Count").

## Dead Code / Removed

Removed in the most recent audit (all confirmed zero-caller via `deadcode` + manual grep before deletion):
- `broker.LoadFromDir` (unused directory-loader variant of `LoadFromFile`)
- `browser.Browser.{GetPageHTML,GetPageTitle,WaitForNavigation,EnablePageEvents}` (unused chromedp wrappers)
- `browser.DetectCaptchaFromHTML` (static-HTML CAPTCHA detection; the live `detectCaptcha` via chromedp is what's actually used)
- `browser.FormFiller.{FillDropdown,CheckCheckbox,DetectFormFields}` and the `FormField` struct (unused form-filling helpers beyond what `Fill()` actually calls)
- `history.Store.GetLastRequestForBroker`
- `inbox.{ClassifyBatch,FilterByType,GetActionableResponses,GetBouncedResponses}`
- `inbox.Monitor.MoveToFolder`
- `inbox.ExtractConfirmationToken`
- `template.Engine.AvailableTemplates`
- `web.SessionStore.Count` (no metrics/debug endpoint ever called it)
- `internal/web/templates/setup.html` (top-level) and `forms.html` - both unreferenced by any handler, superseded by `setup/welcome.html` and `tasks.html` respectively
- `internal/web/templates/partials/{history-list,task-list}.html` and their handlers `handleAPIHistory` (`GET /api/history`), `handleAPITasks` (`GET /api/pipeline/tasks`), `handleAPIStats` (`GET /api/stats`), `handleAPIPipelineStats` (`GET /api/pipeline/stats`) - none were ever called from any template; dashboard/pipeline stats are rendered server-side directly instead

Also removed: the `github.com/sendgrid/sendgrid-go` and `github.com/resend/resend-go/v2` dependencies (dead - `email.NewSender` has only ever supported `smtp`, and the setup wizard never offered those providers as options) and the `stretchr/testify` indirect dependency (was only pulled in transitively by the packages above). `go.mod`/`go.sum` cleaned via `go mod tidy`.

If you're re-adding one of these, check git history for the implementation rather than rewriting from scratch - they weren't broken, just unreferenced.
