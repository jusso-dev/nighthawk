# Nighthawk architecture

## 1. What Herdr really exposes (verified against Herdr 0.9.3 source)

Checked in `herdrdev/herdr` at 0.9.3 (`src/api/schema/events.rs`, `panes.rs`,
`agents.rs`, `integrations.rs`) and the reference plugin
`jusso-dev/herdr-sched-cluster` (`herdr-plugin.toml`, `docs/GAP-ANALYSIS.md`).

| Herdr surface | What Nighthawk uses it for | Limit |
| --- | --- | --- |
| Plugin manifest `[[build]]`, `[[startup]]`, `[[actions]]`, `[[panes]]`, `[[events]]` | Install, daemon start, palette actions, popup TUI | Startup is one-shot. No shutdown hook. No plugin storage API. |
| Manifest `[[events]]` hooks, for example `pane.created`, `pane.exited`, `pane.closed` | Learn about panes appearing and dying | Fire-and-forget scripts with env only (`HERDR_PANE_ID`), after the fact |
| Socket `events.subscribe` with `pane.output_matched`, `pane.agent_status_changed`, `pane.scroll_changed` | Live regex matches on pane output, agent working, idle or blocked | Not durable across restarts. Output is rendered terminal text, not structured tool calls. |
| Lifecycle events `pane.agent_detected`, `pane.created`, `workspace.*`, `tab.*`, `worktree.*` | Building the session, pane and agent map | Observational only |
| `pane read`, `agent read` | Pulling recent scrollback for context and evidence | Text only, may be truncated |
| `pane process-info` | PIDs of the agent and its children, for kill and attribution | Point in time |
| `pane report-agent-session` (agent session id) | Linking a pane to an agent's own session id and so its transcript file | Only when the agent integration reports it |
| `pane send-keys`, `agent send-keys` | Pausing a pane (Ctrl-C, Esc) | Best effort, agent-specific |
| `workspace close`, pane close | Killing a session | Kills processes, loses in-memory state |
| `herdr integration install <claude, codex, grok, ...>` | Herdr already installs status hooks into agents; we sit beside them, not in place of them | Herdr's hooks report status, not tool calls |
| `herdr notification` | Asking a human | Desktop only |
| `herdr --machine <id>` | Cluster reach, same as herdr-sched-cluster | Only API-backed verbs, no exec |

**What does not exist:** there is no Herdr hook that runs before an agent's tool
call, no way to veto a command from Herdr, no structured view of MCP traffic and
no network visibility. Herdr can see and stop; it cannot intercept. Every
blocking decision is therefore made at one of three agent-side points:

1. **Agent native hooks.** Claude Code `PreToolUse` and `PostToolUse` (and
   `UserPromptSubmit`, `SessionStart`, `Stop`) call `nighthawk hook claude`,
   which can return a deny decision. Codex is configured through
   `~/.codex/config.toml` (sandbox and approval policy, MCP servers pointed at the
   proxy, and its notify hook). Grok Build and others are handled where they
   expose hooks, and otherwise fall back to proxies plus Herdr observation. Each
   agent's hook surface must be re-verified against its current docs before the
   matching issue is built.
2. **MCP proxy.** `nighthawk mcp-proxy` wraps each MCP server (stdio child or
   streamable HTTP upstream), logs every JSON-RPC message and can deny
   `tools/call`, and pins `tools/list` to detect tampering.
3. **Egress proxy.** `nighthawk egress-proxy` is an HTTP CONNECT proxy set
   through `HTTPS_PROXY` and `HTTP_PROXY` in the agent's environment, with a host
   allowlist. It logs SNI and host only (no TLS interception by default). An agent
   that ignores proxy variables is a known gap, closed on Linux by an optional
   network namespace or nftables mode in M3.

## 2. Components

```
cmd/nighthawk           one static binary: daemon, hook, mcp-proxy, egress-proxy, tui, audit
internal/event          normalised Event type
internal/herdr          Herdr CLI and socket client (subscribe, read, send-keys, close)
internal/capture/...    claude, codex, grok hook adapters; transcript tailer
internal/mcpproxy       stdio and streamable HTTP proxy
internal/egress         CONNECT proxy and allowlist
internal/cost           token parsing and price table
internal/policy         YAML rules, evaluator, default unattended ruleset
internal/detect         detection rules and optional LLM judge
internal/audit          hash chain, SQLite store, verify
internal/redact         secret redaction
internal/alert          desktop, Slack, webhook
internal/export         OpenTelemetry and SIEM forwarders
internal/ui             TUI and local web dashboard
internal/cluster        herdr-sched-cluster integration
```

## 3. Data flow

1. A capture path builds an `event.Event` with `Pre=true` for a pending action.
2. It sends the event to the daemon over a `0600` unix socket in the state
   directory (`$XDG_STATE_HOME/nighthawk/nighthawk.sock`).
3. The daemon redacts, evaluates policy, writes the decision and the event to the
   audit log, and answers allow or deny within a deadline (default 2 s).
4. If the daemon cannot be reached or misses the deadline, the caller applies the
   fail rule: unattended blocks, attended allows with a warning (ADR 0004).
5. Side effects (pause pane, kill session, notify) run asynchronously from the
   decision.

## 4. Storage

SQLite (`modernc.org/sqlite`, pure Go, as in herdr-sched-cluster) at
`$XDG_STATE_HOME/nighthawk/nighthawk.db`, WAL mode. Tables: `events`,
`decisions`, `audit` (hash chain), `sessions`, `usage`, `approvals`. See ADR 0003.

## 5. Modes

- **Attended:** observe everything, block only `critical` rules, ask a human for
  the rest.
- **Unattended:** default ruleset enforced, unknown is denied, approvals time out
  to deny, components fail closed.
