# ADR 0001: Capture strategy

Status: accepted

## Context
Herdr 0.9.3 exposes plugin event hooks, `events.subscribe` (output matches and
agent status) and pane controls, but nothing that runs before an agent's tool
call. Blocking needs a pre-action veto.

## Decision
Use four capture paths, with Herdr as the session spine:
1. Herdr events and pane output for the session, pane and agent map, and for
   after-the-fact stops.
2. Agent native hooks (Claude Code `PreToolUse` and `PostToolUse`, Codex config
   and notify, Grok Build where available) for pre-action veto.
3. An MCP proxy for MCP traffic.
4. An HTTP(S) egress proxy for network traffic.
Token usage comes from hook payloads and transcript files.

## Consequences
Agents without pre-tool hooks get observation plus proxy enforcement only. Hook
installation changes agent config files, so it is a user-invoked action.
