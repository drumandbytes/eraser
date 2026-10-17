#!/usr/bin/env bash
# Download a published release asset, check it against checksums.txt, and run it.
# release.yml runs this on every OS and architecture it ships.
#   scripts/release-smoke.sh v1.4.0 darwin_arm64.tar.gz
#   scripts/release-smoke.sh v1.4.0 windows_amd64.exe
set -euo pipefail

tag=${1:?usage: release-smoke.sh <tag> <asset suffix>}
suffix=${2:?usage: release-smoke.sh <tag> <asset suffix>}
version=${tag#v}
asset="eraser_${version}_${suffix}"

cd "$(mktemp -d)"
gh release download "$tag" --repo "${GITHUB_REPOSITORY:-drumandbytes/eraser}" --pattern "$asset" --pattern checksums.txt

expected=$(awk -v a="$asset" '$2 == a { print $1 }' checksums.txt)
[ -n "$expected" ] || { echo "checksums.txt lists no $asset"; exit 1; }
if command -v sha256sum >/dev/null; then
  actual=$(sha256sum "$asset" | cut -d' ' -f1)
else
  actual=$(shasum -a 256 "$asset" | cut -d' ' -f1)
fi
[ "$expected" = "$actual" ] || { echo "checksum mismatch for $asset"; exit 1; }

case "$asset" in
  *.tar.gz) tar -xzf "$asset"; bin=./eraser ;;
  *.exe) bin="./$asset" ;;
  *) echo "unknown asset type $asset"; exit 1 ;;
esac
chmod +x "$bin" 2>/dev/null || true

out=$("$bin" --version)
echo "$out"
echo "$out" | grep -q "$version" || { echo "version output does not mention $version"; exit 1; }
"$bin" --help >/dev/null
"$bin" validate-brokers
echo "ok: $asset"
