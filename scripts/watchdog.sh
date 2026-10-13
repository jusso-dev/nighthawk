#!/usr/bin/env bash
# Detached loop. Restarts the daemon if it dies. Herdr does not keep this process.
set -euo pipefail
# shellcheck disable=SC1091
source "$(dirname "$0")/lib.sh"
root="$(nh_root)"
state="$(state_dir)"
mkdir -p "$state"
pidfile="${state}/watchdog.pid"
echo $$ >"$pidfile"
trap 'rm -f "$pidfile"; exit 0' TERM INT
interval="${NIGHTHAWK_WATCHDOG_INTERVAL:-2}"
while true; do
  if ! pid_alive "${state}/daemon.pid"; then
    nohup "${root}/bin/nighthawk" daemon >>"${state}/daemon.log" 2>&1 &
  fi
  sleep "$interval"
done
