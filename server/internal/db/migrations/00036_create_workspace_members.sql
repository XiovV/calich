-- +goose Up
-- workspace_members is a WorkspaceMember (ADR-0044): the membership row
-- carrying a User's Workspace Role — Owner, Admin, or Member — inside one
-- Workspace. The Owner of a Workspace also holds a Member row here with
-- role 'owner', so "who is in this Workspace" is always one query.
CREATE TABLE workspace_members (
    workspace_id INTEGER NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role TEXT NOT NULL CHECK (role IN ('owner', 'admin', 'member')),
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (workspace_id, user_id)
);

-- Drives "which Workspaces does this User belong to" — the workspace
-- switcher's data source.
CREATE INDEX idx_workspace_members_user_id ON workspace_members(user_id);

-- +goose Down
DROP TABLE workspace_members;
