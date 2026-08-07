-- +goose Up
-- calendar_shares is the Share (ADR-0034): the grant binding one Calendar
-- to one User with one Role. The Owner never has a row here — ownership
-- stays implicit in calendars.user_id, so a Calendar's Access is always
-- Owner first, then a Share's role, then None (ResolveAccess).
CREATE TABLE calendar_shares (
    calendar_id TEXT NOT NULL REFERENCES calendars(id) ON DELETE CASCADE,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role TEXT NOT NULL CHECK (role IN ('viewer', 'editor')),
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (calendar_id, user_id)
);

-- Drives "which Calendars are shared with me" (ListSharedWithUser), the
-- other half of a Calendar list alongside calendars.user_id.
CREATE INDEX idx_calendar_shares_user_id ON calendar_shares(user_id);

-- +goose Down
DROP TABLE calendar_shares;
