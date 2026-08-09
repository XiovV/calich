-- +goose Up
-- ADR-0044: a Workspace Invite mirrors ADR-0042's account-level Invite
-- mechanics (hashed, single-use, expiring) but targets an email plus a
-- Workspace rather than living on a User row, since the invited address may
-- not have a User yet. UNIQUE(workspace_id, email) means a workspace has at
-- most one outstanding invite per email at a time — issuing a second one for
-- the same pair is a reissue, not a new row.
CREATE TABLE workspace_invites (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    workspace_id INTEGER NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    email TEXT NOT NULL,
    invite_token_hash TEXT NOT NULL,
    invite_expires_at TIMESTAMP NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (workspace_id, email)
);

CREATE UNIQUE INDEX idx_workspace_invites_token_hash ON workspace_invites(invite_token_hash);

-- +goose Down
DROP TABLE workspace_invites;
