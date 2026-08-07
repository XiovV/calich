-- +goose Up
-- calendar_user_colors is a per-User colour override (ADR-0038, amending
-- ADR-0029): calendars.color stays the Owner's and the default every new
-- Share inherits, but any User with Access may shadow it here with a
-- colour that applies to them alone. Keyed directly on (calendar_id,
-- user_id) — unlike user_event_reminders, there's no wholesale-replace to
-- survive, so the override needs no indirection through another table.
CREATE TABLE calendar_user_colors (
    calendar_id TEXT NOT NULL REFERENCES calendars(id) ON DELETE CASCADE,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    color TEXT NOT NULL,
    PRIMARY KEY (calendar_id, user_id)
);

-- +goose Down
DROP TABLE calendar_user_colors;
