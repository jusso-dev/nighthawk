#!/usr/bin/env bash
# Detached loop. Restarts the daemon if it dies. Herdr does not keep this process.
# A mkdir lock keeps one watchdog per state dir; macOS has no flock(1).
set -euo pipefail
# shellcheck disable=SC1091
source "$(dirname "$0")/lib.sh"
root="$(nh_root)"
state="$(state_dir)"
umask 077
mkdir -p "$state"
pidfile="${state}/watchdog.pid"
lock="${state}/watchdog.lock"
if ! mkdir "$lock" 2>/dev/null; then
  pid_alive "${lock}/pid" && exit 0
  # The holder may still be writing its pid. Give it a moment before calling it stale.
  sleep 1
  pid_alive "${lock}/pid" && exit 0
  mv "$lock" "${lock}.stale.$$" 2>/dev/null || exit 0
  rm -rf "${lock}.stale.$$"
  mkdir "$lock" 2>/dev/null || exit 0
fi
echo $$ >"${lock}/pid"
echo $$ >"$pidfile"
trap 'rm -rf "$lock"; rm -f "$pidfile"' EXIT
trap 'exit 0' TERM INT
interval="${NIGHTHAWK_WATCHDOG_INTERVAL:-2}"
while true; do
  if ! pid_alive "${state}/daemon.pid"; then
    nohup "${root}/bin/nighthawk" daemon >>"${state}/daemon.log" 2>&1 &
  fi
  sleep "$interval"
done
