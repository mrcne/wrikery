-- SQLite cannot widen a check constraint, so the outbox is rebuilt to admit task_create.
-- The rows keep their ids and the sequence is carried over by hand: the copy alone would restart
-- the sequence at the highest row left, and a reused id would let a local:N name two things.
CREATE TABLE outbox_new (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    kind            TEXT NOT NULL CHECK (kind IN
                        ('task_create', 'task_update', 'comment_create', 'timelog_create',
                         'timelog_update', 'timelog_delete')),
    entity_id       TEXT NOT NULL,
    payload         TEXT NOT NULL,
    state           TEXT NOT NULL DEFAULT 'pending' CHECK (state IN
                        ('pending', 'inflight', 'failed')),
    attempts        INTEGER NOT NULL DEFAULT 0,
    last_error      TEXT NOT NULL DEFAULT '',
    next_attempt_at TEXT,
    created_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
INSERT INTO outbox_new (id, kind, entity_id, payload, state, attempts, last_error, next_attempt_at, created_at)
    SELECT id, kind, entity_id, payload, state, attempts, last_error, next_attempt_at, created_at FROM outbox;
DELETE FROM sqlite_sequence WHERE name = 'outbox_new';
INSERT INTO sqlite_sequence (name, seq) SELECT 'outbox_new', seq FROM sqlite_sequence WHERE name = 'outbox';
DROP TABLE outbox;
ALTER TABLE outbox_new RENAME TO outbox;
CREATE INDEX idx_outbox_state ON outbox(state);
