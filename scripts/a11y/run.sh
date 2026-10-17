#!/usr/bin/env bash
# Builds the website and the web UI against fictional data, then runs the
# axe-core scan over both. usage: scripts/a11y/run.sh [chrome-binary]
set -euo pipefail
root=$(cd "$(dirname "$0")/../.." && pwd)
work=$(mktemp -d)
pids=()
trap 'kill "${pids[@]}" 2>/dev/null || true; rm -rf "$work"' EXIT

chrome=${1:-${CHROME:-$(command -v google-chrome || command -v chromium || echo "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome")}}

"$root/site/build.sh" -d "$work/site" >/dev/null
(cd "$root" && go build -o "$work/eraser" ./cmd/eraser && go run ./scripts/a11y/seed "$work/home")
(cd "$root/scripts/a11y" && npm ci --silent)

python3 -m http.server 4000 --directory "$work/site" >/dev/null 2>&1 & pids+=($!)
HOME="$work/home" "$work/eraser" serve -p 3999 >"$work/serve.log" 2>&1 & pids+=($!)
for port in 4000 3999; do
  for _ in $(seq 50); do curl -fs -o /dev/null "localhost:$port/" && break; sleep 0.2; done
done

site=http://localhost:4000 ui=http://localhost:3999
node "$root/scripts/a11y/scan.mjs" "$chrome" \
  $site/ $site/brokers/ $site/guides/ $site/docs/next/ $site/docs/next/mcp/ $site/authorities/ $site/privacy/ \
  $ui/ $ui/brokers $ui/pipeline "$ui/pipeline?view=actions" $ui/history $ui/tasks $ui/settings $ui/automation
