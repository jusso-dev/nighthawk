# Config

File: `$NIGHTHAWK_CONFIG`, else `$XDG_CONFIG_HOME/nighthawk/config.yaml`, else `~/.config/nighthawk/config.yaml`.

A missing file loads the defaults below. Unknown keys fail with a line number. Values that start with `env:NAME` are read from the environment at load. `nighthawk config print` masks secret fields.

```yaml
mode_default: attended          # attended or unattended
decision_timeout: 2s
policy_files: []                # empty uses the embedded default rules
prices_file: ""                 # empty uses the embedded USD price list
currency_rate_aud: 0            # 0 disables AUD. A fixed rate, not a live feed.
egress:
  port: 8765
  allowlist:
    - api.anthropic.com
    - api.openai.com
    - api.x.ai
    - claude.ai
    - github.com
    - api.github.com
    - registry.npmjs.org
    - pypi.org
    - files.pythonhosted.org
    - proxy.golang.org
    - sum.golang.org
  allow_private: []             # CIDRs that may resolve to private addresses
alerts:
  slack_webhook: ""             # env:SLACK_WEBHOOK is typical
  webhook_url: ""
  webhook_secret: ""            # HMAC key for X-Nighthawk-Signature
otel:
  endpoint: ""                  # empty disables export
  protocol: http                # http or grpc
redact:
  extra: []                     # additional regexes, replaced with [REDACTED]
anchor:
  key_path: ""                  # ed25519 private key. Must sit outside the state dir.
  webhook_url: ""
  file: ""
  git_repo: ""
  interval: 1h
judge:
  enabled: false
  endpoint: ""                  # OpenAI-compatible chat completions. Empty means off.
  model: ""
  api_key: ""
siem:
  target: ""
  format: ndjson
  syslog_addr: ""
  webhook_url: ""
web:
  addr: 127.0.0.1:8766
mcp_http:
  addr: ""
linux_egress: false             # nftables redirect. Apply only as root on Linux.
```

State directory: `$NIGHTHAWK_STATE_DIR`, else `$XDG_STATE_HOME/nighthawk`, else `~/.local/state/nighthawk`.

| Path | Mode | Contents |
| --- | --- | --- |
| `nighthawk.db` | 0600 | events, decisions, audit chain |
| `nighthawk.sock` | 0600 | daemon IPC |
| `mode` | 0600 | `attended` or `unattended` |
| `daemon.pid` | 0600 | daemon pid |
| `daemon.lock` | 0600 | flock held by the live daemon |
