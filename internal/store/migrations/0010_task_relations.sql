-- A task's super tasks and dependency ids come with the task, the way its folders do.
-- The subtasks of a task are the tasks that name it as a super task, nothing is stored on the parent:
-- adding a subtask leaves the parent's updatedDate alone on Wrike, so a list kept on the parent would go stale.
ALTER TABLE tasks ADD COLUMN attachment_count INTEGER NOT NULL DEFAULT 0;

CREATE TABLE task_supertasks (
    task_id  TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    super_id TEXT NOT NULL,
    PRIMARY KEY (task_id, super_id)
);
CREATE INDEX idx_task_supertasks_super ON task_supertasks(super_id);

CREATE TABLE task_dependencies (
    task_id       TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    dependency_id TEXT NOT NULL,
    PRIMARY KEY (task_id, dependency_id)
);
CREATE INDEX idx_task_dependencies_dependency ON task_dependencies(dependency_id);

-- One row per edge, fetched by id after the pull. An end can be a task outside the cache.
CREATE TABLE dependencies (
    id             TEXT PRIMARY KEY,
    predecessor_id TEXT NOT NULL,
    successor_id   TEXT NOT NULL,
    relation_type  TEXT NOT NULL DEFAULT '',
    lag_minutes    INTEGER NOT NULL DEFAULT 0
);

-- The poll asks only for tasks changed since the cursor, so a task already in the cache would never bring its relations.
-- Clearing the cursors makes the next cycle pull every followed scope from the start, once.
UPDATE scopes SET cursor = NULL, last_synced_at = NULL;
