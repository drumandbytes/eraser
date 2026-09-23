# Contributing to Eraser

Thanks for taking the time to contribute. The most helpful things:

- **Adding brokers** — the database at `data/brokers.yaml` can always use more entries; broker contact details also go stale
- **Privacy authorities** — `data/authorities.yaml` isn't exhaustive and links move; corrections and additions welcome
- **Template improvements** — better wording for removal requests
- **Bug fixes** — found something broken? PRs welcome
- **Documentation** — typos, clarifications, better examples

Open an issue first for anything non-trivial (a feature, a refactor, a behavior change) so we can agree on the approach before you put in the work. Small, obvious fixes (typos, a dead link, a one-line bug) can go straight to a pull request.

## Development setup

Needs [Go](https://go.dev/dl/). Then:

```bash
go build -o eraser ./cmd/eraser
go vet ./...
go test ./...
```

[docs/architecture.md](docs/architecture.md), [docs/commands.md](docs/commands.md), and [docs/code-patterns.md](docs/code-patterns.md) cover the tech stack, project layout, and conventions/known quirks worth reading before touching `send`, the reply classifier, history scoping, or the web UI.

## Adding or updating a broker

`data/brokers.yaml` is compiled into the binary, so a downloaded `eraser` is self-contained. Each entry:

```yaml
- id: example-broker
  name: Example Broker
  email: privacy@example.com
  website: https://example.com
  opt_out_url: https://example.com/optout
  region: us  # us, eu, or global
  category: people-search  # people-search, marketing, background-check, financial-b2b, data-intermediary, device-id-only, or requires-id
```

`./eraser add-broker` does this interactively. Before opening a PR, run:

```bash
go run ./cmd/eraser validate-brokers
```

This checks ids, names, regions, emails, and URLs, and enforces a 700+ broker floor. If you're editing an entry in response to a broker's reply to an actual removal request, [docs/broker-replies.md](docs/broker-replies.md) has the classification table (which reply types need `email: ""`, a `notes:` line, or removing the entry entirely).

## Commits and pull requests

- [Conventional Commits](https://www.conventionalcommits.org/) with a scope — `feat(brokers):`, `fix(site):`, `fix(audit):`, `chore(ci):`, `docs:`. [release-please](.release-please-manifest.json) reads these to version releases and write `CHANGELOG.md`; an unscoped or non-conventional message just won't show up there.
- Work on a branch, open a PR against `main`. PRs are squash-merged, so the PR title becomes the commit message — make it a clean Conventional Commit line.
- All CI checks must pass (`go build`, `go vet`, `go test`, plus broker-list validation).
- Update documentation and tests alongside the code.

This is a single-maintainer project, so PRs aren't blocked on a second approval, but they are reviewed before merge — expect a first response within a few days.

## Reporting a security issue

See the org-wide [Security Policy](https://github.com/drumandbytes/.github/blob/main/SECURITY.md) — please don't open a public issue for a vulnerability. See the [Security Notes](README.md#security-notes) section of the README for how Eraser itself handles your credentials and personal data locally.

## License

Contributions are accepted under this repository's [MIT license](LICENSE).
