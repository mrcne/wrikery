-- When a comment entered the cache, what the inbox marks a row against: comments do not arrive in creation order,
-- a task cached late or a thread read on an open brings older comments after newer ones were seen.
-- The rows read before count as arrived when they were written, the only date they have.
-- A cache from before the inbox reads the month of comments again, four windows of seven days, so the rows carry their mentions,
-- a fresh cache has no task yet and keeps the first run's own window.
ALTER TABLE comments ADD COLUMN arrived_at TEXT NOT NULL DEFAULT '';
UPDATE comments SET arrived_at = created_date;
INSERT OR REPLACE INTO meta (key, value)
SELECT 'comments_pulled_to', strftime('%Y-%m-%dT%H:%M:%SZ', 'now', '-28 days') WHERE EXISTS (SELECT 1 FROM tasks);
