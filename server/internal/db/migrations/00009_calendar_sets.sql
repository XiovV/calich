-- +goose Up
-- calendar_sets and calendar_set_members are a Calendar Set (#301, ADR-0082,
-- CONTEXT.md's Calendar sets entry): a named, private selection of one
-- Workspace's Calendars belonging to one User. Shaped after groups and
-- group_members verbatim (ADR-0048's append-only era), with a second scoping
-- column groups doesn't need: a Group belongs to a Workspace alone, while a
-- Calendar Set is private to one User within one Workspace, so both
-- user_id and workspace_id are carried the way calendars itself carries
-- both. No name-uniqueness constraint, ordered by created_at then id.
--
-- A Set is private outright — no sharing mechanism, no Role, no owner-vs-
-- grantee axis — so the read path is WHERE user_id = ? AND workspace_id = ?
-- and "can B see A's Set?" is not a question the code can ask.
--
-- calendar_set_members cascades on calendars: a revoked Share, an
-- unsubscribe, a Calendar deletion or a disconnected Connection shrinks any
-- Set the Calendar was in, with no reconciliation code and no notification.
-- An emptied Set survives.
CREATE TABLE calendar_sets (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    workspace_id INTEGER NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_calendar_sets_user_id ON calendar_sets(user_id);
CREATE INDEX idx_calendar_sets_workspace_id ON calendar_sets(workspace_id);

CREATE TABLE calendar_set_members (
    set_id INTEGER NOT NULL REFERENCES calendar_sets(id) ON DELETE CASCADE,
    calendar_id TEXT NOT NULL REFERENCES calendars(id) ON DELETE CASCADE,
    PRIMARY KEY (set_id, calendar_id)
);

CREATE INDEX idx_calendar_set_members_calendar_id ON calendar_set_members(calendar_id);

-- +goose Down
DROP TABLE calendar_set_members;
DROP TABLE calendar_sets;
