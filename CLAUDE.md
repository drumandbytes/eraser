# CLAUDE.md

## Project Overview

Eraser is a CLI + web tool that sends data removal requests to data brokers. This is a maintained fork of [digisamroc/eraser](https://github.com/digisamroc/eraser) (inactive since early 2026), hosted at `drumandbytes/eraser`, oriented toward an EU/EEA user exercising GDPR Article 17 rights rather than the original US-only CCPA use case, with optional support for a second household profile. See [EU-NOTES.md](EU-NOTES.md) for the GDPR-specific setup and broker notes, and [README.md](README.md) for user-facing docs.

## Docs Map

This file is intentionally slim. Read the doc(s) relevant to what you're touching rather than everything up front:

- [docs/architecture.md](docs/architecture.md) - tech stack, project structure, broker/template/send-flow concepts
- [docs/commands.md](docs/commands.md) - full CLI command reference, config.yaml schema
- [docs/broker-replies.md](docs/broker-replies.md) - reading data-broker email replies and turning each response type into the right brokers.yaml edit
- [docs/multi-profile.md](docs/multi-profile.md) - the `--profile`/`profiles:` feature: config model, history scoping, shared-inbox attribution, web UI switcher
- [docs/code-patterns.md](docs/code-patterns.md) - conventions to follow, plus known quirks/gotchas worth reading before touching related code
- [docs/auditing.md](docs/auditing.md) - how to re-run the dead-code/security sweep, and what's already been removed

## Quick Start

```bash
go build -o eraser ./cmd/eraser
go vet ./...
go test ./...
```

See [docs/commands.md](docs/commands.md) for the full CLI reference.

## Conventions

- **Commits**: Conventional Commits with a scope - `feat(brokers):`, `fix(site):`, `fix(audit):`, `chore(ci):`, `docs:`. [release-please](.release-please-manifest.json) reads them to bump the version and write `CHANGELOG.md`; an unscoped or non-conventional message just won't appear in the changelog. No `Co-Authored-By` / attribution trailers.
- **Branches & PRs**: work on a branch, open a PR, squash-merge. `main` stays releasable; patch-level release PRs auto-merge.
- **Go code**: idioms, package layout, and known quirks are in [docs/code-patterns.md](docs/code-patterns.md) - read the "Known quirks" section before touching `send`, the reply classifier, history scoping, or the web UI's CSS.

## Recurring work

- **Acting on broker email replies** - the frequent one: classify the reply, make the matching `data/brokers.yaml` edit. [docs/broker-replies.md](docs/broker-replies.md).
- **Keeping `data/brokers.yaml` honest** - liveness audit, dedup, growing the list from registries, and the "750+" count floor: [docs/auditing.md](docs/auditing.md).
- **Re-sending** - brokers re-list you continuously. `./eraser send` is safe to re-run: it resumes where it left off (25-day per-broker cooldown, `daily_send_limit` cap). EU cadence and hand-sending are in [EU-NOTES.md](EU-NOTES.md).
