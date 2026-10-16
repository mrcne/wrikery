-- A sent or discarded row now stays for thirty days with the values it replaced, the changes screen reads it.
-- SQLite cannot widen the check constraint on state, so the table is rebuilt the way 0006 did it:
-- the rows keep their ids and the sequence is carried over by hand, a reused id would let a local:N name two things.
CREATE TABLE outbox_new (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    kind            TEXT NOT NULL CHECK (kind IN
                        ('task_create', 'task_update', 'comment_create', 'timelog_create',
                         'timelog_update', 'timelog_delete')),
    entity_id       TEXT NOT NULL,
    payload         TEXT NOT NULL,
    before          TEXT NOT NULL DEFAULT '',
    task_id         TEXT NOT NULL DEFAULT '',
    task_title      TEXT NOT NULL DEFAULT '',
    source          TEXT NOT NULL DEFAULT '',
    state           TEXT NOT NULL DEFAULT 'pending' CHECK (state IN
                        ('pending', 'inflight', 'failed', 'sent', 'discarded')),
    attempts        INTEGER NOT NULL DEFAULT 0,
    last_error      TEXT NOT NULL DEFAULT '',
    next_attempt_at TEXT,
    created_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    done_at         TEXT
);
INSERT INTO outbox_new (id, kind, entity_id, payload, state, attempts, last_error, next_attempt_at, created_at)
    SELECT id, kind, entity_id, payload, state, attempts, last_error, next_attempt_at, created_at FROM outbox;
DELETE FROM sqlite_sequence WHERE name = 'outbox_new';
INSERT INTO sqlite_sequence (name, seq) SELECT 'outbox_new', seq FROM sqlite_sequence WHERE name = 'outbox';
DROP TABLE outbox;
ALTER TABLE outbox_new RENAME TO outbox;
CREATE INDEX idx_outbox_state ON outbox(state);
CREATE INDEX idx_outbox_entity_state ON outbox(entity_id, state);
-- The rows queued before this migration get their task the way the enqueues record it from now on.
UPDATE outbox SET task_id = entity_id WHERE kind IN ('task_update', 'comment_create', 'timelog_create');
UPDATE outbox SET task_id = 'local:' || id WHERE kind = 'task_create';
UPDATE outbox SET task_id = COALESCE((SELECT task_id FROM timelogs WHERE timelogs.id = outbox.entity_id), '')
    WHERE kind IN ('timelog_update', 'timelog_delete');
