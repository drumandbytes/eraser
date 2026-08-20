# Auditing This Repo

This codebase gets periodic dead-code/security sweeps. To repeat one:

```bash
go build ./... && go vet ./... && go test ./...
go run honnef.co/go/tools/cmd/staticcheck@latest ./...
go run golang.org/x/tools/cmd/deadcode@latest ./...      # unreachable-from-main functions
go run golang.org/x/vuln/cmd/govulncheck@latest ./...    # known CVEs in reachable code paths
```

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
