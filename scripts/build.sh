#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
mkdir -p bin
if command -v go >/dev/null 2>&1; then
  go build -o bin/nighthawk ./cmd/nighthawk
  exit 0
fi
url="${NIGHTHAWK_RELEASE_URL:-}"
sum="${NIGHTHAWK_RELEASE_SHA256:-}"
if [[ -z "$url" || -z "$sum" ]]; then
  echo "go is not installed and NIGHTHAWK_RELEASE_URL is unset" >&2
  exit 1
fi
tmp="$(mktemp)"
curl -fsSL "$url" -o "$tmp"
echo "${sum}  ${tmp}" | shasum -a 256 -c -
install -m 0755 "$tmp" bin/nighthawk
rm -f "$tmp"
