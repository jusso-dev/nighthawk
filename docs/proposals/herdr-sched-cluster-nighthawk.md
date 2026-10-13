# Proposal: herdr-sched-cluster + Nighthawk spend

Per-worker Nighthawk. Each cluster worker runs its own daemon. Spend is queried
with `nighthawk spend --by task --json`; task keys match agent names
`hsc-<job id>`.

When a worker is unreachable, cluster status is `unknown` and `spend` is JSON
`null` (not a zero payload).

No remote PR into herdr-sched-cluster from this change. That other repo is not
present in this workspace; this document only records the intended field map and
query shape for operators wiring both sides later.
