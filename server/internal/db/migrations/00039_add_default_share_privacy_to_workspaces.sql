-- +goose Up
-- default_share_privacy is a per-Workspace setting an Owner/Admin configures
-- (ADR-0044, #156) — stored now, not yet consumed by anything. A later
-- Calendar/Group ticket (ADR-0045) will read it as the default privacy a
-- newly shared Calendar gets within the Workspace.
ALTER TABLE workspaces ADD COLUMN default_share_privacy TEXT NOT NULL DEFAULT 'private';

-- +goose Down
ALTER TABLE workspaces DROP COLUMN default_share_privacy;
