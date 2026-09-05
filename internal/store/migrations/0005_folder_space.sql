-- Space root folders come marked by the API ("space": true on the folder object, https://developers.wrike.com/api/v4/folders-projects/).
-- The sidebar starts its tree from them.
ALTER TABLE folders ADD COLUMN space INTEGER NOT NULL DEFAULT 0;
