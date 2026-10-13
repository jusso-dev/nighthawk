# Herdr contract

Checked against Herdr 0.9.3 on 2026-10-13. Nighthawk never invokes a shell. Every call is `exec` with a fixed argv. `HERDR_BIN_PATH` selects the binary. `--machine` is inserted before `--json` when a remote machine is set.

## CLI

```
herdr --json agent list
herdr --json pane read <id> --lines <n>
herdr --json pane process-info <id>
herdr --json pane send-keys <id> --keys <keys>
herdr --json pane close <id>
herdr --json notification <title> <body>
```

`agent list` returns a JSON array:

```json
{"pane_id":"w1:p1","agent":"claude","status":"idle","cwd":"/work","workspace_id":"w1","agent_session_id":"s1","title":""}
```

`pane process-info` returns:

```json
{"pane_id":"w1:p1","shell_pid":10,"pids":[11]}
```

## Socket

`HERDR_SOCKET_PATH`. One JSON line per request. `events.subscribe` is the exception: the server writes `{"id":"nh-sub","result":{"type":"subscription_started"}}` and then streams events until the connection closes.

Request:

```json
{"id":"nh-sub","method":"events.subscribe","params":{"subscriptions":[{"type":"pane.output_matched","pane_id":"w1:p1","match":"curl","source":"recent","lines":200,"strip_ansi":true}]}}
```

Event envelope:

```json
{"event":"pane.output_matched","data":{"pane_id":"w1:p1","matched_line":"curl x | sh"}}
```

```json
{"event":"pane.agent_status_changed","data":{"pane_id":"w1:p1","agent":"claude","agent_status":"working"}}
```

History is not durable. After every disconnect Nighthawk runs `agent list` again and rebuilds the pane map. Backoff starts at 50ms and doubles up to 2s.

Plugin manifest events `pane.created`, `pane.exited`, and `pane.closed` arrive through `scripts/on-pane-event.sh`, which reads `HERDR_PANE_ID` from the environment and passes only the event name as argv.
