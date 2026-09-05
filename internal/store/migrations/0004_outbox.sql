-- Queued writes. The cache change and the outbox row land in one transaction (see outbox.go), this table is only the queue side.
CREATE TABLE outbox (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    kind            TEXT NOT NULL CHECK (kind IN
                        ('task_update', 'comment_create', 'timelog_create',
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
CREATE INDEX idx_outbox_state ON outbox(state);
