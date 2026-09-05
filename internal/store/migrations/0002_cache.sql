-- The cache schema. Ids are Wrike ids.
-- Timestamps are RFC3339 UTC strings, they sort correctly as text.
-- Task dates and timelog tracked_date stay the API zone-less strings, see the spec on why they are never parsed.

CREATE TABLE tasks (
    id                TEXT PRIMARY KEY,
    title             TEXT NOT NULL DEFAULT '',
    description       TEXT NOT NULL DEFAULT '',
    description_plain TEXT NOT NULL DEFAULT '',
    status            TEXT NOT NULL DEFAULT '',
    custom_status_id  TEXT NOT NULL DEFAULT '',
    importance        TEXT NOT NULL DEFAULT '',
    permalink         TEXT NOT NULL DEFAULT '',
    dates_type        TEXT,
    dates_duration    INTEGER,
    dates_start       TEXT,
    dates_due         TEXT,
    created_date      TEXT NOT NULL DEFAULT '',
    updated_date      TEXT NOT NULL DEFAULT '',
    last_opened_at    TEXT
);
CREATE INDEX idx_tasks_updated ON tasks(updated_date);

CREATE TABLE task_responsibles (
    task_id    TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    contact_id TEXT NOT NULL,
    PRIMARY KEY (task_id, contact_id)
);
CREATE INDEX idx_task_responsibles_contact ON task_responsibles(contact_id);

CREATE TABLE task_parents (
    task_id   TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    folder_id TEXT NOT NULL,
    PRIMARY KEY (task_id, folder_id)
);
CREATE INDEX idx_task_parents_folder ON task_parents(folder_id);

CREATE TABLE folders (
    id                       TEXT PRIMARY KEY,
    title                    TEXT NOT NULL DEFAULT '',
    scope                    TEXT NOT NULL DEFAULT '',
    is_project               INTEGER NOT NULL DEFAULT 0,
    project_status           TEXT,
    project_custom_status_id TEXT,
    project_start_date       TEXT,
    project_end_date         TEXT
);

CREATE TABLE folder_children (
    parent_id TEXT NOT NULL,
    child_id  TEXT NOT NULL,
    PRIMARY KEY (parent_id, child_id)
);

CREATE TABLE spaces (
    id          TEXT PRIMARY KEY,
    title       TEXT NOT NULL DEFAULT '',
    access_type TEXT NOT NULL DEFAULT '',
    archived    INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE contacts (
    id            TEXT PRIMARY KEY,
    first_name    TEXT NOT NULL DEFAULT '',
    last_name     TEXT NOT NULL DEFAULT '',
    type          TEXT NOT NULL DEFAULT '',
    primary_email TEXT NOT NULL DEFAULT '',
    deleted       INTEGER NOT NULL DEFAULT 0,
    me            INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE workflows (
    id       TEXT PRIMARY KEY,
    name     TEXT NOT NULL DEFAULT '',
    standard INTEGER NOT NULL DEFAULT 0,
    hidden   INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE custom_statuses (
    id           TEXT PRIMARY KEY,
    workflow_id  TEXT NOT NULL REFERENCES workflows(id) ON DELETE CASCADE,
    name         TEXT NOT NULL DEFAULT '',
    color        TEXT NOT NULL DEFAULT '',
    status_group TEXT NOT NULL DEFAULT '',
    standard     INTEGER NOT NULL DEFAULT 0,
    hidden       INTEGER NOT NULL DEFAULT 0,
    position     INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE comments (
    id           TEXT PRIMARY KEY,
    task_id      TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    author_id    TEXT NOT NULL DEFAULT '',
    text         TEXT NOT NULL DEFAULT '',
    created_date TEXT NOT NULL DEFAULT ''
);
CREATE INDEX idx_comments_task ON comments(task_id);

-- No foreign key to tasks on purpose. Own timelogs can point at tasks that
-- were never cached or left the cache, and the timesheet still lists them.
CREATE TABLE timelogs (
    id              TEXT PRIMARY KEY,
    task_id         TEXT NOT NULL,
    user_id         TEXT NOT NULL DEFAULT '',
    category_id     TEXT NOT NULL DEFAULT '',
    tracked_date    TEXT NOT NULL DEFAULT '',
    comment         TEXT NOT NULL DEFAULT '',
    hours           REAL NOT NULL DEFAULT 0,
    lock_status     TEXT NOT NULL DEFAULT '',
    approval_status TEXT NOT NULL DEFAULT '',
    created_date    TEXT NOT NULL DEFAULT '',
    updated_date    TEXT NOT NULL DEFAULT ''
);
CREATE INDEX idx_timelogs_task ON timelogs(task_id);
CREATE INDEX idx_timelogs_user_date ON timelogs(user_id, tracked_date);

CREATE TABLE scopes (
    scope_id       TEXT PRIMARY KEY,
    kind           TEXT NOT NULL CHECK (kind IN ('space', 'project', 'me')),
    title          TEXT NOT NULL DEFAULT '',
    followed       INTEGER NOT NULL DEFAULT 1,
    cursor         TEXT,
    last_synced_at TEXT
);
