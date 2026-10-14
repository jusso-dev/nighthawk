# Herdr contract

Checked against Herdr 0.9.3 (`herdr <cmd> --help`, `herdr api schema --json`, live `agent list` and `events.subscribe`) on 2026-10-14. Nighthawk never invokes a shell. Every call is `exec` with a fixed argv. `HERDR_BIN_PATH` selects the binary. `--machine <label>` is inserted first when a remote machine is set.

## CLI

There is no `--json` flag; Herdr rejects it with `unknown option: --json`. API commands print one JSON response on stdout. Failures exit non-zero and print an error body on stdout:

```json
{"error":{"code":"pane_not_found","message":"pane not found"},"id":"cli:pane:process_info"}
```

```
herdr agent list
herdr pane read <id> --lines <n>          # plain text, not JSON
herdr pane process-info --pane <id>
herdr pane send-keys <id> <key>...        # logical keys: esc, ctrl+c
herdr pane close <id>
herdr notification show <title> --body <text>
```

`agent list` returns `result.agents`. `agent` and `agent_session` may be null. `agent_session.value` is the agent's own session id (or path when `kind` is `path`). Fields Nighthawk reads:

```json
{"id":"cli:agent:list","result":{"type":"agent_list","agents":[{"pane_id":"w1:p1","workspace_id":"w1","tab_id":"w1:t1","terminal_id":"term-1","agent":"claude","agent_status":"working","cwd":"/work","agent_session":{"source":"herdr:claude","agent":"claude","kind":"id","value":"s1"},"focused":false,"revision":7}]}}
```

`agent_status` is one of `idle`, `working`, `blocked`, `done`, `unknown`.

`pane process-info` returns the shell pid and the foreground process group. `shell_pid` may be null:

```json
{"id":"cli:pane:process_info","result":{"type":"pane_process_info","process_info":{"pane_id":"w1:p1","shell_pid":10,"foreground_process_group_id":11,"foreground_processes":[{"pid":11,"name":"claude","argv":["claude"],"cwd":"/work"}]}}}
```

Kill sends SIGTERM only to foreground pids that descend from `shell_pid`, waits 3s, re-checks descent for each pid, then sends SIGKILL, then `pane close`.

## Socket

`HERDR_SOCKET_PATH`. One JSON line per request. `events.subscribe` is the exception: the server writes `{"id":"nh-sub","result":{"type":"subscription_started"}}` and then streams events until the connection closes. An invalid request or unknown pane gets an error line instead, for example `{"id":"","error":{"code":"invalid_request","message":"..."}}`; Nighthawk logs it, resyncs, and redials with backoff.

Request. `pane.output_matched` requires `pane_id`, `source` and `match`; `match` is an internally tagged `OutputMatch` (`{"type":"regex"|"substring","value":...}`), not a string. `pane.agent_detected` is global and takes only `type`:

```json
{"id":"nh-sub","method":"events.subscribe","params":{"subscriptions":[{"type":"pane.agent_detected"},{"type":"pane.agent_status_changed","pane_id":"w1:p1"},{"type":"pane.output_matched","pane_id":"w1:p1","match":{"type":"regex","value":"curl"},"source":"recent","lines":200,"strip_ansi":true}]}}
```

Event envelopes. Per-pane subscriptions use the dotted name:

```json
{"event":"pane.output_matched","data":{"pane_id":"w1:p1","matched_line":"curl x | sh","read":{}}}
```

```json
{"event":"pane.agent_status_changed","data":{"pane_id":"w1:p1","workspace_id":"w1","agent":"claude","agent_status":"working"}}
```

Lifecycle events use the underscore name. `agent` is null when the pane's agent is released; Nighthawk then records the pane as `unknown`. Nighthawk accepts both `pane_agent_detected` and `pane.agent_detected`:

```json
{"event":"pane_agent_detected","data":{"type":"pane_agent_detected","pane_id":"w1:p1","workspace_id":"w1","agent":"claude","released":false,"final_status":null}}
```

A detection updates the pane map and rebuilds the per-pane subscriptions.

History is not durable. After every disconnect Nighthawk runs `agent list` again and rebuilds the pane map. Backoff starts at 50ms and doubles up to 2s.

Plugin manifest events `pane.created`, `pane.exited`, and `pane.closed` arrive through `scripts/on-pane-event.sh`, which reads `HERDR_PANE_ID` from the environment and passes only the event name as argv.
