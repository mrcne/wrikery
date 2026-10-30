-- The last per task read of the comments and time entries, NULL for a thread never read.
-- The polls read the thread of every opened task, so a task the user opened starts with the stamp of its open.
ALTER TABLE tasks ADD COLUMN thread_read_at TEXT;
UPDATE tasks SET thread_read_at = last_opened_at WHERE last_opened_at IS NOT NULL;
