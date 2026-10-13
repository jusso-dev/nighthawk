#!/usr/bin/env bash
# Shared path helpers. state_dir matches internal/config.StateDir.
set -euo pipefail

state_dir() {
  if [[ -n "${NIGHTHAWK_STATE_DIR:-}" ]]; then
    printf '%s\n' "$NIGHTHAWK_STATE_DIR"
    return
  fi
  if [[ -n "${XDG_STATE_HOME:-}" ]]; then
    printf '%s\n' "${XDG_STATE_HOME}/nighthawk"
    return
  fi
  printf '%s\n' "${HOME}/.local/state/nighthawk"
}

nh_root() {
  cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd
}

pid_alive() {
  local f="$1"
  [[ -f "$f" ]] || return 1
  local pid
  pid="$(cat "$f" 2>/dev/null || true)"
  [[ -n "${pid}" ]] && kill -0 "${pid}" 2>/dev/null
}
