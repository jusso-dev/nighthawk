#!/usr/bin/env bash
set -euo pipefail
bin="${HERDR_BIN_PATH:-herdr}"
exec "$bin" plugin pane open --plugin nighthawk --entrypoint "${1:-dashboard}"
