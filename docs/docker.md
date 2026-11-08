# Running Eraser in Docker

The image runs every Eraser command, including the ones that drive a browser
(`fill` and the `mcp` browser tools): it ships headless Chromium. Your config,
history and screenshots live in a volume, so the container can be recreated or
upgraded without losing anything.

## Quick start

```bash
git clone https://github.com/drumandbytes/eraser.git
cd eraser
docker compose up -d
```

Open <http://localhost:8080> and follow the setup wizard, the same as the
desktop app ([getting-started.md](getting-started.md)). Stop it with
`docker compose down`; your data stays in the `eraser-data` volume.

Any CLI command runs the same way:

```bash
docker compose run --rm eraser send --dry-run
docker compose run --rm eraser status
docker compose run --rm eraser fill --pending        # headless Chromium
docker compose exec eraser eraser mcp pending        # while the web UI is running
```

Interactive commands (`init`, `profile add`, `passphrase set`, `mcp approve`)
work too: `docker compose run --rm eraser init`.

Without compose:

```bash
docker build -t eraser:local .
docker run -d --name eraser -p 127.0.0.1:8080:8080 --shm-size=1g \
  -v eraser-data:/home/eraser/.eraser eraser:local
```

## Security: keep the port on 127.0.0.1

> [!WARNING]
> The web UI has no login, and it can send mail as you with the credentials it
> holds. Always publish the port as `127.0.0.1:8080:8080`, never `8080:8080` or
> `0.0.0.0:8080:8080`: those expose it to everyone on your network (and, on a
> server, possibly the internet).

Inside the container the server listens on `0.0.0.0` (`serve --listen 0.0.0.0`,
the image's default command) because Docker can't forward to a container's
loopback. It prints a warning saying so. Requests whose `Host` header isn't
`localhost`/`127.0.0.1`/`::1` are still refused, as on the desktop, but that's a
guard against DNS rebinding, not authentication. To reach the UI from another
machine, use an SSH tunnel (`ssh -L 8080:localhost:8080 server`) rather than
publishing the port.

## Where your data lives

Everything is in `/home/eraser/.eraser` inside the container, which is the
`eraser-data` named volume:

| File | What |
|---|---|
| `config.yaml` | profile(s), SMTP/IMAP settings, encrypted passwords |
| `history.db` | every request sent and reply received |
| `machine-id` | the key material for the encrypted passwords (see below) |
| `screenshots/` | `fill` screenshots |
| `brokers.yaml` | written by `update-brokers` |

Back it up with:

```bash
docker run --rm -v eraser-data:/data -v "$PWD":/backup debian:trixie-slim \
  tar czf /backup/eraser-backup.tgz -C /data .
```

Prefer the named volume to a bind mount of a host folder. On Windows and macOS
a bind mount can't keep the file permissions Eraser checks (it warns when
`config.yaml` is readable by others), and on Linux the folder must be owned by
uid 1000.

### Encrypted passwords and `machine-id`

Eraser encrypts `smtp.password` and `inbox.password` with a key derived from
the machine ID and the user (see "Security Notes" in the README). A container
has no `/etc/machine-id` of its own and gets a new hostname on every run, so the
image links `/etc/machine-id` to `machine-id` in the volume, created once by the
entrypoint. Recreating the container or upgrading the image keeps the same key.

This means the key sits in the same volume as the config it unlocks: a copied
`config.yaml` alone is still useless, but a copy of the whole volume can be
decrypted. For protection against that, set a passphrase
(`docker compose run --rm eraser passphrase set`). The trade-off is the same as
on the desktop: `serve` asks for it in the web UI after every container start,
and runs with nobody at the keyboard can't unlock the passwords until you do.

## Browser automation (Chromium)

`fill` and the `mcp` browser tools use the bundled Chromium, always headless:
there's no screen in a container. So:

- `fill --headless=false` and `fill --wait` (solve a CAPTCHA by hand in the
  browser window) aren't available. A form with a CAPTCHA becomes a manual
  task in the web UI, as it does in headless mode on the desktop.
- `pipeline.browser_headless: false` in your config doesn't apply here; pass
  `--headless` to be explicit if you share a config with a desktop install.
- `eraser mcp` shows Chrome by default for verification handoff. In Docker,
  run it with `--headless`.

The image sets `ERASER_CONTAINER=1`, which (among other things, see
Scheduling below) starts Chromium with `--no-sandbox` and
`--disable-dev-shm-usage`. Chromium's own sandbox needs user
namespaces, which Docker's default seccomp profile doesn't give an unprivileged
process; the container (non-root user, no extra capabilities) is the isolation
boundary instead. Eraser's own limits on browser automation still apply: it only
navigates to the selected broker's domains (see [mcp.md](mcp.md)). Compose sets
`shm_size: 1gb` as well, since Docker's 64 MB `/dev/shm` is small for Chromium.

## Using `eraser mcp` from an agent

An MCP client launches the server over stdio. Point it at:

```bash
docker run -i --rm -v eraser-data:/home/eraser/.eraser --shm-size=1g \
  eraser:local mcp --headless
```

Approve batches in the web UI's Automation page (with `docker compose up`
running against the same volume) or with
`docker compose run --rm eraser mcp approve <id>`.

## Scheduling

`eraser schedule install` sets up launchd or systemd on the host, which doesn't
exist inside the container, so the image hides that option (`ERASER_CONTAINER=1`
makes `schedule install` and the web UI's "Schedule with my OS" unavailable).
Use one of these instead:

- **In-app scheduler**: keep `docker compose up -d` running and click
  Settings → Automation → "Run while this app is open" (`schedule.enabled: true`).
  `serve` runs a cycle every 6 hours; `restart: unless-stopped` in
  `compose.yaml` keeps it running across reboots.
- **Host cron**: `docker compose -f /path/to/eraser/compose.yaml run --rm eraser auto --once`.

## Upgrading

```bash
git pull
docker compose up -d --build
```

The volume is untouched. `history.db` and `config.yaml` migrate on first run,
as on the desktop ([stability.md](stability.md)).

## Timezone

The image defaults to UTC. Set `TZ` in `compose.yaml` (e.g. `Europe/Berlin`) so
times in the UI and logs match your clock.
