# ADR 0003: Storage

Status: accepted

## Decision
SQLite through `modernc.org/sqlite` (pure Go, no cgo), WAL mode, single writer
in the daemon. Unlike herdr-sched-cluster, which keeps a small JSON document,
Nighthawk needs querying and an append-only trail, so SQLite is justified. The
audit table carries `seq`, `prev_hash`, `hash` (SHA-256 over seq, previous hash
and canonical JSON payload). Retention prunes `events` but never `audit`; old
audit segments are sealed with a checkpoint hash instead.
