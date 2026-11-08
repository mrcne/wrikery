# The JSON the commands print

Every command takes `--json`.
Keys are snake case, a list is never null, a time is the RFC 3339 stamp Wrike stores, and the shapes are a contract: a field is added, never renamed or removed.
The one exception is the stored request under `before` and `payload` of `wrikery changes`, see there.

## A task, from `wrikery task list --json`

A list of tasks, each with:

| key | value |
| --- | --- |
| `id` | the task id, `local:N` for a task created here and not on Wrike yet |
| `title` | the title |
| `status` | the status name in the task's workflow |
| `status_group` | Active, Completed, Deferred or Cancelled |
| `importance` | High, Normal or Low |
| `permalink` | the link to the task in the browser |
| `responsibles` | the people on the task, each with `id` and `name` |
| `folders` | the folders, projects and spaces the task is in, each with `id` and `title` |
| `dates` | null, or `type` (Backlog, Milestone or Planned), `start`, `due` and `duration` in minutes |
| `created` | when the task was made |
| `updated` | when Wrike last changed it |
| `pending` | true while a write for the task waits to be sent or is being sent |
| `failed` | true when Wrike refused a write for the task, `wrikery changes` has the error |

## One task, from `wrikery task show T --json`

The task above, plus:

| key | value |
| --- | --- |
| `description_text` | the description as plain text |
| `description_html` | the HTML Wrike stores |
| `description_markdown` | the description as markdown, the text an edit starts from |
| `attachment_count` | how many files hang on the task |
| `super_tasks` | the tasks this one is a subtask of, each with `id` and `title`, the title empty for a task outside the followed spaces |
| `subtasks` | each with `id`, `title`, `status` and `status_group` |
| `predecessors` | the dependencies before it, each with `id`, `title`, `relation` (FinishToStart and the like) and `lag_minutes` |
| `successors` | the dependencies after it, the same shape |
| `thread_synced` | false while the cache never read the task's comments, offline for example |
| `comments` | oldest first, each with `id`, `author_id`, `author`, `created` and `text` |

## A write, from `wrikery task status`, `create`, `edit`, `comment`, `assign`, `unassign` and `move`

The task as above, read back after the write, plus `sent`, true when the write is on Wrike and false when it is queued, which is exit 3.
`wrikery task comment` adds `comment`, the new comment with its `id`, `author_id`, `author`, `created` and `text`.
A write that changed nothing answers the task with `unchanged` true and `sent` false, exit 0.
When the task cannot be read back after the write, the answer is `id`, `sent` and `read_back` false.

## `wrikery changes --json`

A list of rows, the queue first in send order, then the last thirty days newest first.
`--source WORD` keeps the rows of one source and `--limit N` the first N rows.

| key | value |
| --- | --- |
| `id` | the row number |
| `kind` | task_create, task_update, comment_create, timelog_create, timelog_update or timelog_delete |
| `state` | pending, inflight, failed, sent or discarded |
| `source` | tui for the interface, cli for a command, or the word a command found in WRIKERY_SOURCE |
| `entity_id` | the id of the task, comment or time entry the row writes |
| `task_id` | the task the row belongs to |
| `task_title` | the task's title as the cache has it now, or the one at the time of the write once the task left the cache |
| `summary` | one line, the field with its old and new value, as the changes screen shows it |
| `detail` | the values in full, the whole text of a comment for example, where the summary is cut to a line, often the same text, empty for a few kinds |
| `before` | the values the write replaced, in the shape of `payload`, null when it replaced none |
| `payload` | the request as it was queued, in Wrike's own field names such as customStatusId, a shape that can change between versions |
| `error` | the last error from Wrike, empty when none |
| `attempts` | how many times it was sent |
| `created` | when it was queued |
| `done` | when it was sent or discarded, empty while it waits |

## `wrikery sync --json`

`state` after the cycle, `pending` and `failed` are the counts of the queue.

## The `ui` commands with `--json`

`ok` true and `text`, the interface's own line about what it did.

## `wrikery skill --json`

The files of the skill folder as a map, the path under the folder to the text.
`wrikery skill install --json` answers `written`, the folders it wrote.
