-- Custom field definitions and a task's values, for the time budget and later for the other fields.
CREATE TABLE custom_fields (
    id       TEXT PRIMARY KEY,
    title    TEXT NOT NULL DEFAULT '',
    type     TEXT NOT NULL DEFAULT '',
    space_id TEXT NOT NULL DEFAULT ''
);
CREATE TABLE task_custom_fields (
    task_id  TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    field_id TEXT NOT NULL,
    value    TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (task_id, field_id)
);
-- The Effort field, NULL until a read asked for it, see the upsert.
ALTER TABLE tasks ADD COLUMN effort_mode TEXT;
ALTER TABLE tasks ADD COLUMN effort_minutes INTEGER;
