-- +goose Up
-- Workspace (ADR-0044) is the new account-management and billing boundary
-- above User: a named container with an Owner and an opaque
-- subscription_status, replacing the instance-wide Admin. The Owner
-- reference has no ON DELETE behaviour attached here because Ownership
-- always transfers before a User can be deleted (ADR-0044's sole-Owner
-- guard) — a Workspace should never be left pointing at a deleted User.
CREATE TABLE workspaces (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    owner_user_id INTEGER NOT NULL REFERENCES users(id),
    subscription_status TEXT NOT NULL DEFAULT 'none',
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_workspaces_owner_user_id ON workspaces(owner_user_id);

-- +goose Down
DROP TABLE workspaces;
