-- pins marks the spaces, projects and folders the user is working on now, a display filter for the sidebar.
-- Local only, nothing on Wrike knows about it.
CREATE TABLE pins (
    folder_id TEXT PRIMARY KEY
);
