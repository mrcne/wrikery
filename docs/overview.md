# Overview

Wrike TUI is a terminal client for Wrike.
It is made for people who prefer keyboard and terminal over web application.

Two goals drive every design decision:

1. Speed. Moving through the app feels instant, with no spinners. Going from start to the first screen should feel almost instant.
2. Working offline. Browsing works from a local copy of the data, with or without a network. Changes made offline wait in a queue and are sent when the connection is back.

## Scope for v1

Reading:

- browse the spaces, projects and folders you follow
- task list and task detail: description, status, assignee, dates, comments
- instant full text search over everything cached
- a timesheet view of your own logged time
- a board over workflow statuses, plain or with a lane per person or per folder

Writing:

- add comments
- change task status, assignee and dates
- log, edit and delete your own time entries

Not in v1: creating tasks, editing descriptions, managing subtasks, editing custom fields, dashboards, Gantt charts.
The tool does not try to replace the web application for heavy project management.

## UX

### Layout

Three panes: a sidebar with the followed spaces and projects, a task list and a detail pane for the selected task.
A wide terminal shows all three panes, a medium one shows two, and a terminal narrower than eighty columns shows one, so the app stays usable in a small tmux split.
The visible panes always include the one in focus, so moving focus can slide the window forward or back by one pane.
A status bar at the bottom shows the sync state, an offline indicator and the number of pending or failed writes.
When Wrike rejects a request the bar says the sync is failing instead of offline, since the network is fine and the log has the reason.

### Keys

Vim style keys (j, k, h, l, g, G) and arrow keys both work. `?` shows a help overlay with every binding for the current screen.
`/` filters the current list as you type. Tab cycles through the panes, Enter goes one level deeper, Esc goes back.
On the selected task, `o` opens it in the browser, `y` copies its permalink, `Y` copies a branch name built from the task, and `i` copies the task id.
`z` shows completed and cancelled tasks in the list, and `R` refreshes from Wrike right away instead of waiting for the next poll.

### Search

ctrl+f opens a quick search over everything cached, backed by the full text index.
Type a few letters and the best matches show up at once, ranked. Enter jumps to the task.

### Task detail

The description comes from Wrike as HTML. It is converted to markdown and rendered in the terminal.
Below it come the metadata and the comment thread. Single key actions on the selected task:
`c` comment, `C` comment in an editor, `t` log time, `s` status, `a` assignee, `d` dates, `e` title.
An action changes the local view at once and is sent to Wrike in the background.
A task with a write still queued shows `(sending)` next to its title, and one whose write failed shows `(failed, ! to review)` there instead.

`c` opens a small text box, ctrl+s or ctrl+d sends the comment and closes the box, esc cancels.
`C` hands the terminal to `$VISUAL` or `$EDITOR` on a temporary file instead, for a longer comment.
Saving and quitting the editor sends the file as the comment, an empty file sends nothing.

`s` lists the statuses of the task's workflow, grouped the way Wrike groups them.
Moving the cursor and pressing enter applies the highlighted status.
When the cache does not know the task's workflow yet, the box says so instead of offering another workflow, because a status from another workflow would move the task onto it.

`a` lists the contacts, narrowed as you type a name.
Space toggles a contact on or off, enter applies the change, esc cancels without one.

`d` shows a start and a due field.
Both take quick words instead of a full date: an ISO date, `today`, `tomorrow`, `yesterday`, a weekday name for the next one, `+Nd` or `-Nd` for a relative day, or a day and month such as `12 sep` for a day this year.
An empty field clears that date. Tab moves between the two fields, enter saves, esc cancels.
Saving lets Wrike recompute the duration from the new dates, a custom duration set elsewhere is not kept.

`t` opens a box with hours, date and note fields.
Hours take `1.5`, `1,5`, `1:30`, `90m`, `2h` or `2h30m`.
The date takes the same quick words as the dates dialog and defaults to today.
The note is optional.
Tab moves between the fields, enter saves, esc cancels.
Editing an entry cannot clear a note that is already there.

`e` opens a box with the title, enter saves, esc cancels.
An empty title is refused in the box, an unchanged one closes it without a write.

### Views

The task list can be grouped, and the same tasks can be shown as a board.
`v` cycles the grouping: none, by folder, by assignee, by status.
By folder groups the tasks of a space or project by the folder or project one level below it, so a project reads by its epics, and one sidebar step down regroups by that epic's own children.
On My tasks the group is the followed space or project the task sits in.
By assignee puts you first and unassigned tasks last, a task with several people shows under each of them, and the assignee column shows the status name instead.
Each group starts with a line that carries its name and count, `{` and `}` jump between groups.
`H` and `L` move the selected task to the previous or next status of its workflow without opening the status box.

`b` turns the list into a board: a column per status of the workflow the tasks use, cards in the columns, and a lane per group when a grouping is on.
So the board by assignee is a standup, who is on what and what is stuck, and the board by folder is a board per epic.
Tasks on another workflow than most of the folder gather in one column named after that workflow, and `H` and `L` refuse to move them, the status box still works.
The board takes the width of the window, the sidebar and the task detail show while they have focus: `shift+tab` brings the sidebar, `enter` on a card opens the task on the right, `esc` gives the board the width back.
Columns that do not fit slide in as the cursor moves towards them, an empty column shrinks to its name.
`h` and `l` move between columns, `j` and `k` between cards, `/`, `z` and every task key work as in the list.

A card shows the title on one line, or on two when it is long, and the break prefers a `: ` or ` - ` in the second half of the first line, so `(MX) Backend: Kafka - Processing` reads as a heading and a detail.
A code in parentheses at the start of a title, `(MX)`, is drawn muted and a short part prefix closed by `: ` or ` - `, `Backend: Kafka - `, slightly muted, on cards and rows alike, so the eye lands on the words that differ between tasks.

### Sync issues

`!` opens the list of writes the sync engine could not send.
Each row shows the task title, a short summary of the write (a comment, a status, assignee or dates change, a time entry) and the error Wrike returned.
`r` retries the highlighted row, `x` asks for confirmation and then discards it, enter opens the task, esc goes back to the previous screen.
Discarding a task update does not roll back the change already applied to the local cache, the next refresh brings back whatever Wrike has.

### Timesheet

A separate view (key T) shows the current week as a grid: tasks as rows, Monday to Sunday as columns.
A cell is the hours logged on that task on that day, one decimal, or a dash when there is nothing.
Each row carries a total for the task, each column a total for the day, and the grid ends with a total for the week.
The last row, `+ new task`, is for logging time on a task that has no row yet.

`h` and `l` move between days, `j` and `k` move between rows.
`[` and `]` go one week back and forward, `.` jumps back to the current week.
`n` logs time on the row's task for the selected day.
On the `+ new task` row it opens the quick search first to pick the task, then the time entry box.
`e` or enter edits the entry under the cursor, and when a cell holds more than one entry a small list asks which one first.
`x` deletes an entry after a confirmation, again through that list when the cell holds several.

The sync engine keeps the current week and the eight weeks before it.
An older week shows a not synced line instead of entries, so it does not read like a week with nothing logged.
A cell with a write still queued shows the pending marker in front of the hours.

An entry that sits in a locked or approved timesheet is shown dimmed.
Editing or deleting it is refused with a message instead, nothing is queued for it.

### First run

Paste a Wrike API token. The app checks it and stores it in the system keychain.
Then pick the spaces and projects to follow from a checklist and watch the first sync run.
The goal is less than a minute from install to a working app.

### Demo mode

`wrikery --demo` runs on built in sample data, no token and no network, to try the interface.

### Config

The config file lives at `~/.config/wrikery/config.toml`.
`--config PATH` reads another file instead.
`--no-color` gives plain output without colors, and so does `NO_COLOR` in the environment.
`log_level` sets how much the app logs: debug, info, warn or error.
`poll_interval` sets how often the sync engine checks Wrike for changes.
The `[ui]` table holds `theme` (auto, dark or light), `accent` for the highlight color, and `ascii` to replace drawing glyphs with plain characters on a terminal that cannot show them.
`hide_prefixes` lists title prefixes the rows and cards leave out, for example `["(MX)"]` for a project code every task starts with, the detail pane keeps the full title and the branch name `Y` copies leaves the prefix out too.
`branch_template` builds the branch name that `Y` copies, default `{id}-{slug}`: `{id}` is the task's permalink number and `{slug}` is its title lowercased and cut down to hyphen separated words.
`host` is empty by default, which means the app detects the Wrike data center on first run, set it to `app-eu.wrike.com` to force the EU data center instead.
