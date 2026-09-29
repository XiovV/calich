-- +goose Up
-- tasks is a Task (#310, ADR-0083, CONTEXT.md's Task entry): a unit of work
-- a User tracks, belonging to exactly one Task List and private to its
-- owner. Modelled on VTODO, scoped (user_id, workspace_id) like task_lists
-- (00010_task_lists.sql), and cascading on users, workspaces and
-- task_lists — a Task never outlives the User, Workspace or Task List it
-- belongs to.
--
-- Columns are named for what VTODO holds: title (SUMMARY), notes
-- (DESCRIPTION), due (DUE, the Deadline), start + duration_minutes (DTSTART
-- + DURATION, the Time block — two independent axes per ADR-0083, so
-- neither is derived from the other), priority (PRIORITY, 0-9), completed_at
-- (COMPLETED, nullable, never a status enum). This ticket only exercises
-- title and completed_at; due, start, duration_minutes, priority and notes
-- land now, unused, so a later ticket is a codec-and-backend job rather than
-- a migration (ADR-0083).
--
-- Deleting a Task List reparents its Tasks to the default Task List, in the
-- same transaction, before the Task List row itself is removed
-- (TaskListService.Delete) — so task_list_id's ON DELETE CASCADE below is a
-- safety net for the case that never happens (the default Task List cannot
-- itself be deleted while it holds the flag), not the mechanism reparenting
-- relies on.
CREATE TABLE tasks (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    workspace_id INTEGER NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    task_list_id INTEGER NOT NULL REFERENCES task_lists(id) ON DELETE CASCADE,
    title TEXT NOT NULL,
    notes TEXT NOT NULL DEFAULT '',
    due TIMESTAMP,
    start TIMESTAMP,
    duration_minutes INTEGER,
    priority INTEGER NOT NULL DEFAULT 0,
    completed_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_tasks_user_id ON tasks(user_id);
CREATE INDEX idx_tasks_workspace_id ON tasks(workspace_id);
CREATE INDEX idx_tasks_task_list_id ON tasks(task_list_id);

-- +goose Down
DROP TABLE tasks;
