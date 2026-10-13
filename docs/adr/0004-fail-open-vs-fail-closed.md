# ADR 0004: Fail open or fail closed

Status: accepted

## Decision
- **Unattended:** fail closed. If the daemon is unreachable, slow, or the policy
  errors, hooks and proxies deny. Approvals with no answer deny at timeout.
- **Attended:** fail open with a visible warning, because a human is watching and
  a broken daemon should not stop work.
- The mode lives in the daemon and is cached by each hook in a `0600` file so a
  hook can still fail closed when the daemon is down.

## Consequences
A crashed daemon halts unattended agents. That is intended. A watchdog restarts
the daemon and alerts.
