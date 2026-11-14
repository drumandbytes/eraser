# CLAUDE.md

## Project Overview

Delistd is a CLI + web tool that sends data removal requests to data brokers. It is hosted at `drumandbytes/delistd` and oriented toward an EU/EEA user exercising GDPR Article 17 rights, with optional support for a second household profile. See [EU-NOTES.md](EU-NOTES.md) for the GDPR-specific setup and broker notes, and [README.md](README.md) for user-facing docs.

## Docs Map

This file is intentionally slim. Read the doc(s) relevant to what you're touching rather than everything up front:

- [docs/getting-started.md](docs/getting-started.md) - plain-language setup guide for non-technical users (Gmail app password, wizard, troubleshooting); keep it in step with the setup wizard wording
- [docs/architecture.md](docs/architecture.md) - tech stack, project structure, broker/template/send-flow concepts
- [docs/commands.md](docs/commands.md) - full CLI command reference, config.yaml schema
- [docs/broker-replies.md](docs/broker-replies.md) - reading data-broker email replies and turning each response type into the right brokers.yaml edit
- [docs/multi-profile.md](docs/multi-profile.md) - the `--profile`/`profiles:` feature: config model, history scoping, shared-inbox attribution, web UI switcher
- [docs/docker.md](docs/docker.md) - the Docker image (distroless `Dockerfile`, published to `ghcr.io/drumandbytes/delistd` on each release, `compose.yaml` with the stock `chromedp/headless-shell` Chromium sidecar): `serve --listen`, `DELISTD_CONTAINER`, `DELISTD_CHROME_URL` (remote Chrome), the volume-held `DELISTD_MACHINE_ID_FILE` that keeps encrypted passwords readable across container recreation
- [docs/kubernetes.md](docs/kubernetes.md) - a single-pod Kubernetes manifest for the ghcr image (PVC, `fsGroup`, Chromium as a second container) and what to avoid (exposing the UI, NFS for `history.db`, more than one replica)
- [docs/outlook.md](docs/outlook.md) - Outlook.com/Hotmail/Live sign-in with OAuth (`delistd oauth login`, `internal/oauth`): registering the Microsoft app, what is stored where, known gaps
- [docs/mcp.md](docs/mcp.md) - the experimental `delistd mcp` agent server: batch approval, shared-platform host scope, and what it may and may not do
- [docs/code-patterns.md](docs/code-patterns.md) - conventions to follow, plus known quirks/gotchas worth reading before touching related code
- [docs/auditing.md](docs/auditing.md) - how to re-run the dead-code/security sweep, and what's already been removed
- [docs/stability.md](docs/stability.md) - what 1.x promises not to break (config keys, CLI flags, history.db, brokers.yaml, export JSON) - check before renaming or removing any of them

## Quick Start

```bash
go build -o delistd ./cmd/delistd
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
- **Keeping `data/brokers.yaml` honest** - liveness audit, dedup, growing the list from registries, and keeping the public "700+" broker claim (README, site, setup page) true to the real count: [docs/auditing.md](docs/auditing.md).
- **Removing or merging a broker with a guide page** (`/brokers/<id>/`, curated categories only - see `curatedCategories` in `cmd/delistd/cmd_guides.go`): the next deploy drops the page and the URL 404s via `site/layouts/404.html`. If it was merged into or renamed to another entry, add `/brokers/<old-id>/ /brokers/<new-id>/ 301` to `site/layouts/index.redirects` (the Hugo template that generates Cloudflare Pages' `_redirects`, also the `/docs/` to latest-release 302; a `static/_redirects` would collide with it) so links and search results land on the surviving page. Without a `404.html`, Pages serves the homepage with a 200 for unknown paths.
- **Re-sending** - brokers re-list you continuously. `./delistd send` is safe to re-run: it resumes where it left off (25-day per-broker cooldown, `daily_send_limit` cap); `delistd schedule install` / `delistd auto` run it unattended ([docs/commands.md](docs/commands.md)). EU cadence and hand-sending are in [EU-NOTES.md](EU-NOTES.md).
