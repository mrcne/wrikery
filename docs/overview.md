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
On a narrow terminal the panes collapse into one column and you go one level deeper at a time instead,
so the app stays usable in a small tmux split.
A status bar at the bottom shows the sync state, an offline indicator and the number of pending or failed writes.

### Keys

Vim style keys (j, k, h, l, g, G) and arrow keys both work. `?` shows a help overlay with every binding for the current screen.
`/` filters the current list as you type. Tab cycles through the panes, Enter goes one level deeper, Esc goes back.

### Search

ctrl+f opens a quick search over everything cached, backed by the full text index.
Type a few letters and the best matches show up at once, ranked. Enter jumps to the task.

### Task detail

The description comes from Wrike as HTML. It is converted to markdown and rendered in the terminal.
Below it come the metadata and the comment thread. Single key actions on the selected task:
`c` comment, `t` log time, `s` status, `a` assignee, `d` dates.
An action changes the local view at once and is sent to Wrike in the background.

### Timesheet

A separate view (key T) shows the current week as a grid of your time entries with totals per day and per week.
Arrow keys move between days, one key adds an entry. The view exists to answer one question quickly: did I log everything this week.

### First run

Paste a Wrike API token. The app checks it and stores it in the system keychain.
Then pick the spaces and projects to follow from a checklist and watch the first sync run.
The goal is less than a minute from install to a working app.
