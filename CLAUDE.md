# CLAUDE.md

## Project Overview

Eraser is a CLI + web tool that sends data removal requests to data brokers. This is a maintained fork of [digisamroc/eraser](https://github.com/digisamroc/eraser) (inactive since early 2026), hosted at `drumandbytes/eraser`, oriented toward an EU/EEA user exercising GDPR Article 17 rights rather than the original US-only CCPA use case, with optional support for a second household profile. See [EU-NOTES.md](EU-NOTES.md) for the GDPR-specific setup and broker notes, and [README.md](README.md) for user-facing docs.

## Docs Map

This file is intentionally slim. Read the doc(s) relevant to what you're touching rather than everything up front:

- [docs/architecture.md](docs/architecture.md) - tech stack, project structure, broker/template/send-flow concepts
- [docs/commands.md](docs/commands.md) - full CLI command reference, config.yaml schema
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
