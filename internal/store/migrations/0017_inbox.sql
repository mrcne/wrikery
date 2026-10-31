-- The follow flag of a task and the users a comment mentions, what the inbox reads.
-- The comments cursor goes so the next cycle reads the week of comments again and the rows carry their mentions.
-- The inbox walks the window of the last thirty days through the index, never the whole table.
ALTER TABLE tasks ADD COLUMN followed_by_me INTEGER NOT NULL DEFAULT 0;
ALTER TABLE comments ADD COLUMN mentions TEXT NOT NULL DEFAULT '';
CREATE INDEX idx_comments_created ON comments(created_date);
DELETE FROM meta WHERE key = 'comments_pulled_to';
