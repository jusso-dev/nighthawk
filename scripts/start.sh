#!/usr/bin/env bash
# One-shot Herdr startup. The daemon and watchdog detach. This process exits.
set -euo pipefail
# shellcheck disable=SC1091
source "$(dirname "$0")/lib.sh"
root="$(nh_root)"
state="$(state_dir)"
if [[ ! -x "${root}/bin/nighthawk" ]]; then
  "${root}/scripts/build.sh"
fi
# State and logs are private: dir 0700, files 0600.
umask 077
mkdir -p "$state"
chmod 700 "$state"
chmod 600 "${state}"/*.log 2>/dev/null || true
if ! pid_alive "${state}/daemon.pid"; then
  nohup "${root}/bin/nighthawk" daemon >>"${state}/daemon.log" 2>&1 &
  for _ in 1 2 3 4 5 6 7 8 9 10; do
    pid_alive "${state}/daemon.pid" && break
    sleep 0.1
  done
fi
if ! pid_alive "${state}/watchdog.pid"; then
  nohup "${root}/scripts/watchdog.sh" >>"${state}/watchdog.log" 2>&1 &
fi
exit 0
