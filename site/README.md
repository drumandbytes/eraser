# site/

The docs site — [eraser.drumandbytes.dev](https://eraser.drumandbytes.dev),
built with [Hugo](https://gohugo.io) and deployed by
`.github/workflows/pages.yml` on every push to `main` that touches the broker
data, `site/`, or the guide generator.

## Build locally

```bash
./site/build.sh            # or: ./site/build.sh serve
```

That runs `eraser guides` to generate `content/brokers/*.md` +
`static/brokers.json`, copies `data/eu-dpas.yaml` in, and runs `hugo --minify`
into `site/public/`. All generated files are gitignored.

## Layout

- `content/` — hand-written pages (`_index.md`, `guides/*.md`, `authorities.md`);
  `content/brokers/*.md` is generated.
- `layouts/` — the theme, ~6 small templates.
- `static/css/site.css` — one stylesheet; its palette mirrors the app's light
  tokens (`internal/web/static/css/tokens.css`).
- `static/CNAME` — the custom domain.
