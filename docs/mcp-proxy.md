# MCP proxy

`internal/mcpproxy` sits between an agent and an MCP server. It logs JSON-RPC,
can deny `tools/call`, and pins `tools/list` hashes so a server cannot quietly
change a tool after approval.

## Transports

### Stdio

```
nighthawk mcp-proxy --server NAME -- cmd args...
```

The proxy spawns `cmd` in its own process group, speaks newline-delimited
JSON-RPC on stdin/stdout, and copies child stderr to the proxy stderr. On
cancel it kills the process group so grandchildren die with the child.

### Streamable HTTP (2025-11-25)

As of the 2025-11-25 MCP streamable HTTP shape, clients use POST for JSON-RPC,
may open an SSE stream, and carry `Mcp-Session-Id`. This proxy still accepts
that shape: it forwards `Mcp-Session-Id` and `Authorization` on POST, and
answers denied `tools/call` locally without dialing upstream.

The 2026-07-28 MCP spec dropped session ids plus GET/DELETE session routes.
Nighthawk keeps the 2025-11-25 session header for clients that still send it.

## Policy

Each `tools/call` becomes an `event.Event` with `Kind=mcp_call`,
`Source=mcp_proxy`, and attrs `mcp_server`, `mcp_tool`, `args`. A non-allow
decision returns JSON-RPC error `-32001` with message
`blocked by Nighthawk: <rule>`. Arguments are never echoed in the error.
Notifications (no `id`) pass through. JSON-RPC batches drop denied
`tools/call` items; a fully denied batch returns one error and is not
forwarded.

Logs go through `redact.String` and are truncated to 8 KiB. Authorization
headers are forwarded on HTTP but never written to the log line.

## Pins

`nighthawk mcp pins` lists and approves pending tool hashes. `HashTool` is
sha256 of canonical JSON `{name, description, schema}`. `Diff` compares a
`tools/list` result to approved pins: new or changed tools emit a tamper and a
pending hash; in unattended mode they are stripped from the filtered list.
