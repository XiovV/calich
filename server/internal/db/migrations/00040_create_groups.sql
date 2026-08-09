-- +goose Up
-- groups is a Group (ADR-0045): a named set of a Workspace's Members,
-- created and membership-managed only by that Workspace's Owner or Admin.
-- A Group belongs to exactly one Workspace and never moves between them.
CREATE TABLE groups (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    workspace_id INTEGER NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_groups_workspace_id ON groups(workspace_id);

-- group_members is the Group-membership join table (ADR-0045): which Users
-- currently belong to which Group. Membership is resolved dynamically —
-- there is no denormalized copy anywhere else — so a row here is the single
-- source of truth for "is this User in this Group right now".
CREATE TABLE group_members (
    group_id INTEGER NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (group_id, user_id)
);

CREATE INDEX idx_group_members_user_id ON group_members(user_id);

-- +goose Down
DROP TABLE group_members;
DROP TABLE groups;
