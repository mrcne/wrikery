-- The stale list walks the stamped tasks in order of change, every cycle.
-- Without this index the walk reads every row of a large cache, most of them never read.
CREATE INDEX idx_tasks_thread_stale ON tasks(updated_date) WHERE thread_read_at IS NOT NULL;
