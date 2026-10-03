-- A write submitted against a local id after its create landed, an editor saved a minute later for example,
-- would name a row that no longer exists. The completions record the swap here and the enqueues resolve
-- a local target through it, so the write lands on the real id.
CREATE TABLE local_ids (
    local_id TEXT PRIMARY KEY,
    real_id  TEXT NOT NULL
);
