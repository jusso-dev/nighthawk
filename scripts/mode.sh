#!/usr/bin/env bash
set -euo pipefail
# shellcheck disable=SC1091
source "$(dirname "$0")/lib.sh"
root="$(nh_root)"
if [[ $# -lt 1 ]]; then
  exec "${root}/bin/nighthawk" mode get
fi
exec "${root}/bin/nighthawk" mode set "$1"
