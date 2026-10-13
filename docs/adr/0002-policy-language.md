# ADR 0002: Policy language

Status: accepted

## Context
Options were Rego (OPA), CEL, or a small YAML rule format.

## Decision
YAML rules with a fixed set of matchers (kind, agent, tool, command regex, path
glob, host, MCP server and tool) and an optional CEL `when` expression via
`github.com/google/cel-go` in M3. Rego is not embedded: it adds a large
dependency and a language most users will not write. Rules are evaluated in
order, first matching rule wins, with a default action per mode.

## Consequences
Simple to read and test. Complex logic waits for CEL.
