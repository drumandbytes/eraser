# site/

The docs site — [eraser.drumandbytes.dev](https://eraser.drumandbytes.dev/?ref=eraser-site-readme),
built with [Hugo](https://gohugo.io) and deployed to Cloudflare Pages by
`.github/workflows/deploy.yaml` on every push to `main` that touches the broker
data, `site/`, or the guide generator.

## Build locally

```bash
./site/build.sh            # or: ./site/build.sh serve
```

That runs `eraser guides` to generate `content/brokers/*.md` +
`static/brokers.json`, copies `data/authorities.yaml` in, and runs `hugo --minify`
into `site/public/`. All generated files are gitignored.

## Layout

- `content/` — hand-written pages (`_index.md`, `guides/*.md`, `authorities.md`);
  `content/brokers/*.md` is generated.
- `layouts/` — the theme, ~6 small templates.
- `static/css/site.css` — one stylesheet; its palette mirrors the app's light
  tokens (`internal/web/static/css/tokens.css`).
- `static/CNAME` — the custom domain.

## Versioned docs

`/docs/` has one snapshot per release line plus a nightly one, and a beta one
while a prerelease tag is ahead of the last stable release.
`scripts/docs-versions` builds them (`go run ./scripts/docs-versions`, which
`build.sh` and the deploy workflow both run):

- `next` is the working tree, shown as **Nightly**.
- The newest tag of each stable minor line (`1.2`, `1.1`, ...) is a frozen snapshot of
  that tag's `docs/*.md`. The newest is marked latest and is the only one search
  engines may index; every other version is `noindex`.
- A prerelease tag newer than the last stable release (for example
  `v1.5.0-beta.1`) appears as **1.5 beta** until a stable release passes it.
- 0.x tags are skipped.

Only the user-facing pages listed in `scripts/docs-versions/main.go` are published.
A new release triggers a deploy (`release: published`), which adds its snapshot.
The version menu sits in `layouts/partials/docs-nav.html`; the snapshots and
`data/docs_versions.json` are generated and gitignored. The build needs the release
tags, so run `git fetch --tags` first if a version is missing locally.
