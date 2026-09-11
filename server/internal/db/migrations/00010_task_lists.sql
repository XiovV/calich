-- +goose Up
-- task_lists is a Task List (#317, ADR-0083, CONTEXT.md's Task List entry):
-- a named, private container of Tasks belonging to one User within one
-- Workspace. Deliberately not a Calendar (ADR-0083's "why a Task List is
-- not a Calendar") — no Access, Share, Role, Source or Exposure, so the
-- read path is WHERE user_id = ? AND workspace_id = ? and "can B see A's
-- Task List?" is not a question the code can ask. Scoped and shaped after
-- calendar_sets (00009_calendar_sets.sql): both user_id and workspace_id,
-- cascading on both, ordered by created_at then id.
--
-- is_default marks the one Task List a Task with no stated list goes to —
-- exactly one per (user_id, workspace_id), moved atomically by the service
-- layer (clear old, set new) rather than enforced here. Auto-created as
-- "Inbox" the moment a (User, Workspace) pair comes into being (Workspace
-- creation, Workspace Invite acceptance), inside the same transaction that
-- creates the pair (ADR-0018).
CREATE TABLE task_lists (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    workspace_id INTEGER NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    color TEXT NOT NULL,
    is_default BOOLEAN NOT NULL DEFAULT 0,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_task_lists_user_id ON task_lists(user_id);
CREATE INDEX idx_task_lists_workspace_id ON task_lists(workspace_id);

-- +goose Down
DROP TABLE task_lists;
