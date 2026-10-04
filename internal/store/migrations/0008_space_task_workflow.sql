-- A new task starts in the default task workflow of its space, which is not always the account's standard one.
-- The local row a create writes before Wrike answers needs it to show the status the task will really get.
ALTER TABLE spaces ADD COLUMN default_task_workflow_id TEXT NOT NULL DEFAULT '';
