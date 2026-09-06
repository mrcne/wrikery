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
`c` comment, `C` comment in an editor, `t` log time, `s` status, `a` assignee, `d` dates.
An action changes the local view at once and is sent to Wrike in the background.
A task with a write still queued shows `(sending)` next to its title, and one whose write failed shows `(failed, ! to review)` there instead.

`c` opens a small text box, ctrl+s or ctrl+d sends the comment and closes the box, esc cancels.
`C` hands the terminal to `$VISUAL` or `$EDITOR` on a temporary file instead, for a longer comment.
Saving and quitting the editor sends the file as the comment, an empty file sends nothing.

`s` lists the statuses of the task's workflow, grouped the way Wrike groups them.
Moving the cursor and pressing enter applies the highlighted status.

`a` lists the contacts, narrowed as you type a name.
Space toggles a contact on or off, enter applies the change, esc cancels without one.

`d` shows a start and a due field.
Both take quick words instead of a full date: an ISO date, `today`, `tomorrow`, `yesterday`, a weekday name for the next one, `+Nd` or `-Nd` for a relative day, or a day and month such as `12 sep` for a day this year.
An empty field clears that date. Tab moves between the two fields, enter saves, esc cancels.
Saving lets Wrike recompute the duration from the new dates, a custom duration set elsewhere is not kept.

### Sync issues

`!` opens the list of writes the sync engine could not send.
Each row shows the task title, a short summary of the write (a comment, a status, assignee or dates change, a time entry) and the error Wrike returned.
`r` retries the highlighted row, `x` asks for confirmation and then discards it, enter opens the task, esc goes back to the previous screen.
Discarding a task update does not roll back the change already applied to the local cache, the next refresh brings back whatever Wrike has.

### Timesheet

A separate view (key T) shows the current week as a grid of your time entries with totals per day and per week.
Arrow keys move between days, one key adds an entry. The view exists to answer one question quickly: did I log everything this week.

### First run

Paste a Wrike API token. The app checks it and stores it in the system keychain.
Then pick the spaces and projects to follow from a checklist and watch the first sync run.
The goal is less than a minute from install to a working app.

### Demo mode

`wrikery --demo` runs on built in sample data, no token and no network, to try the interface.

### Config

The config file lives at `~/.config/wrikery/config.toml`.
`log_level` sets how much the app logs: debug, info, warn or error.
`poll_interval` sets how often the sync engine checks Wrike for changes.
The `[ui]` table holds `theme` (auto, dark or light), `accent` for the highlight color, and `ascii` to replace drawing glyphs with plain characters on a terminal that cannot show them.
`branch_template` builds the branch name that `Y` copies, default `{id}-{slug}`: `{id}` is the task's permalink number and `{slug}` is its title lowercased and cut down to hyphen separated words.
