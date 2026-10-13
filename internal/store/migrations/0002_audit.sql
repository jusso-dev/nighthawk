CREATE TABLE IF NOT EXISTS audit (
    seq INTEGER PRIMARY KEY,
    ts INTEGER NOT NULL,
    payload BLOB NOT NULL,
    prev_hash TEXT NOT NULL,
    hash TEXT NOT NULL
);

CREATE TRIGGER IF NOT EXISTS audit_no_update
BEFORE UPDATE ON audit
BEGIN
    SELECT RAISE(ABORT, 'audit is append only');
END;

CREATE TRIGGER IF NOT EXISTS audit_no_delete
BEFORE DELETE ON audit
BEGIN
    SELECT RAISE(ABORT, 'audit is append only');
END;
