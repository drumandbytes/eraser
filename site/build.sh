#!/usr/bin/env bash
# Build the docs site locally. CI does the same steps in pages.yml.
#   hugo:  go run github.com/gohugoio/hugo@latest   (or install it)
set -euo pipefail
cd "$(dirname "$0")/.."

go run ./cmd/eraser guides --format md --output site/content
mkdir -p site/data
cp data/authorities.yaml site/data/authorities.yaml

cd site
if command -v hugo >/dev/null 2>&1; then
  hugo --minify "$@"
else
  go run github.com/gohugoio/hugo@latest --minify "$@"
fi
echo "built site/public/"
