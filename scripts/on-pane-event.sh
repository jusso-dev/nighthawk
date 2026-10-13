#!/usr/bin/env bash
# HERDR_PANE_ID comes from the environment. Herdr does not substitute argv.
set -euo pipefail
# shellcheck disable=SC1091
source "$(dirname "$0")/lib.sh"
root="$(nh_root)"
exec "${root}/bin/nighthawk" herdr-event "${1:-}"
