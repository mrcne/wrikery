-- A task fetched from outside the followed scopes carries the time it was asked for or referenced.
-- The sweep keeps such a task for thirty days, a scope pull that lists the task clears the stamp.
ALTER TABLE tasks ADD COLUMN reached_at TEXT;
