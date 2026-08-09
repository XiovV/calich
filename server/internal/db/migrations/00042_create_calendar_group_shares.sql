-- +goose Up
-- calendar_group_shares is the Group-targeted Share (ADR-0045): the grant
-- binding one Calendar to one Group with one Role, alongside calendar_shares'
-- per-User Share. Access resolution takes the best of ownership, a direct
-- Share, and a Share on a Group the User currently belongs to — resolved
-- dynamically off group_members, never expanded into a snapshot here.
CREATE TABLE calendar_group_shares (
    calendar_id TEXT NOT NULL REFERENCES calendars(id) ON DELETE CASCADE,
    group_id INTEGER NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    role TEXT NOT NULL CHECK (role IN ('viewer', 'editor')),
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (calendar_id, group_id)
);

CREATE INDEX idx_calendar_group_shares_group_id ON calendar_group_shares(group_id);

-- +goose Down
DROP TABLE calendar_group_shares;
