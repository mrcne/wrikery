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
