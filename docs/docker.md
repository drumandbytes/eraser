# Running Delistd in Docker

The Delistd image is distroless (about 40 MB): the static binary and nothing
else. It runs every command except the browser ones: the web UI, `send`,
`confirm`, `status`, scheduling. `fill` and the `mcp` browser tools use a
second, optional container with headless Chromium (the stock
`chromedp/headless-shell` image) that Delistd reaches over the DevTools port.

Your config, history and screenshots live in a volume, so containers can be
recreated or upgraded without losing anything.

## Quick start

```bash
git clone https://github.com/drumandbytes/delistd.git
cd delistd
docker compose up -d
```

Open <http://localhost:8080> and follow the setup wizard, the same as the
desktop app ([getting-started.md](getting-started.md)). Stop it with
`docker compose down`; your data stays in the `eraser-data` volume.

Any CLI command runs the same way:

```bash
docker compose run --rm delistd send --dry-run
docker compose run --rm delistd status
docker compose --profile browser run --rm delistd fill --pending   # headless Chromium
docker compose exec delistd delistd mcp pending        # while the web UI is running
```

Interactive commands (`init`, `profile add`, `passphrase set`, `mcp approve`)
work too: `docker compose run --rm delistd init`.

Commands run in the data volume (`/home/nonroot/.eraser`), so files they write
to the current directory, like `export`'s `delistd-evidence-*.html`, are kept
there after a `run --rm` container exits. Copy one out with
`docker cp delistd:/home/nonroot/.eraser/<file> .` while the web UI container is
running, or pass `-o` with a path you can reach.

Without compose, `compose.yaml` is the reference for the flags: a private
network, the loopback-only port, the `eraser-data` volume, and for the browser
`--shm-size=1g` plus `-e DELISTD_CHROME_URL=http://chrome:9222`.

## Security: keep the port on 127.0.0.1

> [!WARNING]
> The web UI has no login, and it can send mail as you with the credentials it
> holds. Always publish the port as `127.0.0.1:8080:8080`, never `8080:8080` or
> `0.0.0.0:8080:8080`: those expose it to everyone on your network (and, on a
> server, possibly the internet).

Inside the container the server listens on `0.0.0.0` (`serve --listen 0.0.0.0`,
the image's default command) because Docker can't forward to a container's
loopback. It prints a warning saying so. Requests whose `Host` header isn't
`localhost`/`127.0.0.1`/`::1` are refused unless they match `DELISTD_ALLOWED_HOSTS`,
a guard against DNS rebinding (not authentication). The image's default is
`lan,local`: a private IP (`192.168.1.5`) or a local name (`tower`, `tower.local`,
anything under `.lan`, `.home.arpa` or `.internal`) works, so Unraid and other LAN
setups work out of the box, while a public hostname is still refused (that is what
a rebinding attack needs). Set `DELISTD_ALLOWED_HOSTS` yourself to change it: a
comma-separated list of `lan` / `private` (private IPs), `local` (local names), IPs,
CIDRs (`10.0.0.0/8`), hostnames (`delistd.example.com`, needed behind a reverse proxy)
and `*.domain` wildcards, or `*` to accept every Host. An empty value, or one that
matches nothing (say `none`), leaves only loopback. You can also reach the UI via an SSH tunnel
(`ssh -L 8080:localhost:8080 server`).

Other containers on the same Docker network can reach the container directly
and send any `Host` header they like, so treat them as trusted. `compose.yaml`
gives Delistd a network of its own; with plain `docker run`, use a dedicated
network as above rather than Docker's default bridge, which every container
shares.

## Where your data lives

Everything is in `/home/nonroot/.eraser` inside the container, which is the
`eraser-data` named volume:

| File | What |
|---|---|
| `config.yaml` | profile(s), SMTP/IMAP settings, encrypted passwords |
| `history.db` | every request sent and reply received |
| `machine-id` | the key material for the encrypted passwords (see below) |
| `screenshots/` | `fill` screenshots |
| `brokers.yaml` | written by `update-brokers` |

Back it up with Delistd stopped, so `history.db` (SQLite in WAL mode) isn't
copied mid-write. Run it from a folder outside the delistd checkout so the
archive doesn't end up next to the source. It holds your config, history and
`machine-id`, so it's created readable by you only:

```bash
docker compose -f /path/to/delistd/compose.yaml stop
docker run --rm -v eraser-data:/data -v "$PWD":/backup debian:trixie-slim \
  sh -c 'umask 077 && tar czf /backup/delistd-backup.tgz -C /data .'
docker compose -f /path/to/delistd/compose.yaml start
```

To restore into a new volume, extract the archive the same way and then run
`chown -R 65532:65532 /data` in that helper container. A volume whose first
mount was a root-owned helper, instead of the Delistd image, is root-owned, and
Delistd then fails with `permission denied` on `history.db`.

Prefer the named volume to a bind mount of a host folder. On Windows and macOS
a bind mount can't keep the file permissions Delistd checks (it warns when
`config.yaml` is readable by others), and on Linux the folder must be owned by
uid 65532.

### Encrypted passwords and `machine-id`

Delistd encrypts `smtp.password` and `inbox.password` with a key derived from
the machine ID and the user (see "Security Notes" in the README). A container
has no `/etc/machine-id` of its own and gets a new hostname on every run, so the
image sets `DELISTD_MACHINE_ID_FILE` to `machine-id` in the volume. Delistd
creates it on first use (safe when several containers start at once) and reuses
it afterwards. Recreating the container or upgrading the image keeps the same
key.

This means the key sits in the same volume as the config it unlocks: a copied
`config.yaml` alone is still useless, but a copy of the whole volume can be
decrypted. For protection against that, set a passphrase
(`docker compose run --rm delistd passphrase set`). The trade-off is the same as
on the desktop: `serve` asks for it in the web UI after every container start,
and the unlock only lasts for that `serve` process. Every other process, which
includes the in-app scheduler's cycles (it starts a separate `delistd auto --once`),
host cron runs and `docker run ... mcp`, can't unlock the passwords on its own,
so a passphrase and unattended sending don't mix.

## Browser automation (Chromium)

`fill` and the `mcp` browser tools drive Chromium in a second container:

```bash
docker compose --profile browser up -d
docker compose --profile browser run --rm delistd fill --pending
```

The image sets `DELISTD_CHROME_URL=http://chrome:9222` (compose passes it), and
Delistd attaches to that DevTools endpoint instead of launching a local Chrome.
The browser container is the stock
[`chromedp/headless-shell`](https://hub.docker.com/r/chromedp/headless-shell)
image, pinned by tag and digest in `compose.yaml`: Chromium without the desktop
UI, the smallest full browser build I found (Alpine's and Browserless's are far larger). It runs with `--no-sandbox`
because Chromium's own sandbox needs user namespaces, which Docker's default
seccomp profile doesn't give an unprivileged process; the container (read-only
filesystem, no capabilities) is the boundary instead. The stock image runs
Chromium as root inside that container. Because Chromium is a
separate container, a browser exploit from a broker page doesn't reach your
Delistd volume or config.

The DevTools port is not published to the host. It controls the browser with
no authentication, so only containers on the compose network can reach it
(treat those as trusted, as with the web UI).

Any DevTools endpoint works, not only this image: set `DELISTD_CHROME_URL` to an
`http://host:9222` or `ws://` URL. That includes a hosted service like
[Browserless](https://github.com/browserless/browserless) (check its license
for your use, and put its token in the URL) or Chrome on your desktop started
with `--remote-debugging-port`. Delistd sets the user agent and window size
itself on a remote browser.

The browser is always headless, so what you lose against the desktop app:

- `fill --wait` (solve a CAPTCHA by hand in the browser window). A form with a
  CAPTCHA becomes a manual task in the web UI, as it does in headless mode on
  the desktop.
- `delistd mcp`'s visible-browser handoff. When the agent stops for a CAPTCHA,
  login or ID step ([mcp.md](mcp.md)), there's no window to finish it in:
  do that broker's form yourself in your own browser and complete the task in
  the web UI's Tasks page. `mcp` in Docker suits the brokers whose forms need
  no intervention.

Delistd's own limits on browser automation still apply: it only navigates to the
selected broker's domains (see [mcp.md](mcp.md)). Compose sets `shm_size: 1gb`
on the browser container, since Docker's 64 MB `/dev/shm` is small for Chromium.

## Using `delistd mcp` from an agent

An MCP client launches the server over stdio. Point it at the same network as
the browser container:

```bash
docker run -i --rm --network delistd_default -e DELISTD_CHROME_URL=http://chrome:9222 \
  -v eraser-data:/home/nonroot/.eraser "${DELISTD_IMAGE:-delistd:local}" mcp
```

Approve batches in the web UI's Automation page (with `docker compose up`
running against the same volume) or with
`docker compose run --rm delistd mcp approve <id>`.

## Scheduling

`delistd schedule install` sets up launchd or systemd on the host, which doesn't
exist inside the container, so the image hides that option (`DELISTD_CONTAINER=1`
makes `schedule install` and the web UI's "Schedule with my OS" unavailable).
Use one of these instead:

- **In-app scheduler**: keep `docker compose up -d` running and click
  Settings → Automation → "Run while this app is open" (`schedule.enabled: true`).
  `serve` runs a cycle every 6 hours; `restart: unless-stopped` in
  `compose.yaml` keeps it running across reboots.
- **Host cron**: `docker compose -f /path/to/delistd/compose.yaml run --rm delistd auto --once`.

## Upgrading

```bash
git pull
docker compose --profile browser up -d --build   # drop --profile browser if you don't use it
```

`--build` rebuilds the Delistd image from the clone; the browser container is a stock image
pinned in `compose.yaml`, so it updates when that pin changes.

The volume is untouched. `history.db` and `config.yaml` migrate on first run,
as on the desktop ([stability.md](stability.md)).

If you run the published image instead, see the next section.

## Using the published image

Each release pushes `ghcr.io/drumandbytes/delistd` (`linux/amd64`, `linux/arm64`)
with the tags `v2.2.0` (exact release), `2.2` (latest patch of that minor,
no `v`) and `latest`. To run it from the same
`compose.yaml` without building, set `DELISTD_IMAGE`:

```bash
export DELISTD_IMAGE=ghcr.io/drumandbytes/delistd:v2.2.0
docker compose up -d --pull always --no-build
```

`--no-build` makes compose fail instead of silently building when the pull
doesn't work. Upgrading is a new tag in `DELISTD_IMAGE` and the same command.
Keep `DELISTD_IMAGE` exported (or in a `.env` file next to `compose.yaml`) for
later `docker compose run` calls, so they use the same image.

## Kubernetes

A pod manifest (PVC, Deployment, Service, Chromium as a second container) is in
[kubernetes.md](kubernetes.md).

## Timezone

The image defaults to UTC. Set `TZ` in `compose.yaml` (e.g. `Europe/Berlin`) so
times in the UI and logs match your clock.
