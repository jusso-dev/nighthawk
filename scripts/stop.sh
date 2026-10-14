#!/usr/bin/env bash
set -euo pipefail
# shellcheck disable=SC1091
source "$(dirname "$0")/lib.sh"
state="$(state_dir)"
stop_pid() {
  local f="$1"
  pid_alive "$f" || return 0
  local pid
  pid="$(cat "$f")"
  kill -TERM "$pid" 2>/dev/null || true
}
stop_pid "${state}/watchdog.pid"
stop_pid "${state}/daemon.pid"
for _ in 1 2 3 4 5 6 7 8 9 10; do
  pid_alive "${state}/daemon.pid" || exit 0
  sleep 0.5
done
if pid_alive "${state}/daemon.pid"; then
  kill -KILL "$(cat "${state}/daemon.pid")" 2>/dev/null || true
fi
exit 0
