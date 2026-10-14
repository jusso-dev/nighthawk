CREATE TABLE IF NOT EXISTS meta (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS events (
    id TEXT PRIMARY KEY,
    ts INTEGER NOT NULL,
    kind TEXT NOT NULL,
    source TEXT NOT NULL,
    machine TEXT,
    pane_id TEXT,
    agent TEXT,
    session_id TEXT,
    task_id TEXT,
    pre INTEGER NOT NULL,
    attrs TEXT
);

CREATE INDEX IF NOT EXISTS events_ts ON events (ts);
CREATE INDEX IF NOT EXISTS events_agent_ts ON events (agent, ts);
CREATE INDEX IF NOT EXISTS events_session ON events (session_id);

CREATE TABLE IF NOT EXISTS decisions (
    id TEXT PRIMARY KEY,
    event_id TEXT NOT NULL,
    ts INTEGER NOT NULL,
    action TEXT NOT NULL,
    rule_id TEXT,
    reason TEXT
);

CREATE TABLE IF NOT EXISTS sessions (
    pane_id TEXT PRIMARY KEY,
    workspace TEXT,
    agent TEXT,
    agent_session_id TEXT,
    cwd TEXT,
    status TEXT,
    started INTEGER,
    ended INTEGER,
    machine TEXT,
    task_id TEXT
);

CREATE TABLE IF NOT EXISTS usage (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    ts INTEGER NOT NULL,
    pane_id TEXT,
    agent TEXT,
    session_id TEXT,
    task_id TEXT,
    model TEXT,
    input_tokens INTEGER NOT NULL DEFAULT 0,
    output_tokens INTEGER NOT NULL DEFAULT 0,
    cache_read INTEGER NOT NULL DEFAULT 0,
    cache_write INTEGER NOT NULL DEFAULT 0,
    message_key TEXT UNIQUE
);

CREATE TABLE IF NOT EXISTS approvals (
    id TEXT PRIMARY KEY,
    event_id TEXT,
    rule_id TEXT,
    status TEXT NOT NULL,
    decided_by TEXT,
    decided_at INTEGER,
    expires_at INTEGER,
    event_json TEXT
);
