-- External content FTS (full text search) for tasks, the pattern from https://sqlite.org/fts5.html#external_content_tables.
-- description_plain is stripped from HTML in Go before the row lands, triggers only copy it.
-- Recovery after any suspected drift: INSERT INTO tasks_fts(tasks_fts) VALUES('rebuild').
CREATE VIRTUAL TABLE tasks_fts USING fts5(
    title,
    description_plain,
    content='tasks',
    content_rowid='rowid'
);

CREATE TRIGGER tasks_fts_ai AFTER INSERT ON tasks BEGIN
    INSERT INTO tasks_fts(rowid, title, description_plain)
    VALUES (new.rowid, new.title, new.description_plain);
END;

CREATE TRIGGER tasks_fts_ad AFTER DELETE ON tasks BEGIN
    INSERT INTO tasks_fts(tasks_fts, rowid, title, description_plain)
    VALUES ('delete', old.rowid, old.title, old.description_plain);
END;

CREATE TRIGGER tasks_fts_au AFTER UPDATE OF title, description_plain ON tasks BEGIN
    INSERT INTO tasks_fts(tasks_fts, rowid, title, description_plain)
    VALUES ('delete', old.rowid, old.title, old.description_plain);
    INSERT INTO tasks_fts(rowid, title, description_plain)
    VALUES (new.rowid, new.title, new.description_plain);
END;
