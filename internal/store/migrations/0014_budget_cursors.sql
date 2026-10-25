-- The poll asks only for tasks changed since the cursor, so a task already in the cache would never bring its custom field values.
-- Clearing the cursors makes the next cycle pull every followed scope from the start, once.
UPDATE scopes SET cursor = NULL, last_synced_at = NULL;
