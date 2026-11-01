-- The unread count of the inbox reads the comments that arrived after the seen marker, through this index.
CREATE INDEX idx_comments_arrived ON comments(arrived_at);
