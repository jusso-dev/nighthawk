CREATE TABLE IF NOT EXISTS usage_offsets (
    path TEXT PRIMARY KEY,
    byte_off INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS skills (
    name TEXT NOT NULL,
    path TEXT NOT NULL,
    hash TEXT,
    first_seen INTEGER,
    last_seen INTEGER,
    uses INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (name, path)
);

CREATE TABLE IF NOT EXISTS mcp_pins (
    server TEXT NOT NULL,
    tool TEXT NOT NULL,
    hash TEXT NOT NULL,
    pending_hash TEXT,
    first_seen INTEGER,
    PRIMARY KEY (server, tool)
);

CREATE TABLE IF NOT EXISTS siem_outbox (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    payload TEXT NOT NULL,
    attempts INTEGER NOT NULL DEFAULT 0,
    next_at INTEGER NOT NULL,
    delivered INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS audit_anchors (
    seq INTEGER NOT NULL,
    hash TEXT NOT NULL,
    target TEXT NOT NULL,
    ts INTEGER NOT NULL,
    sig TEXT NOT NULL,
    PRIMARY KEY (seq, target)
);
