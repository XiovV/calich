-- +goose Up
-- calendar_exposures is Exposure (#297, ADR-0080, CONTEXT.md's Exposure
-- entry): one User's own choice about whether a Calendar appears in *their
-- own* CalDAV home-set. Keyed and shaped exactly like calendar_user_colors
-- — no indirection, cascading on both Calendar and User — because an
-- Exposure override has the same no-wholesale-replace-to-survive shape a
-- colour override does (ADR-0038). Absent a row, the default is resolved in
-- code rather than stored: exposed, except a Linked Calendar's own Owner,
-- who defaults to unexposed.
CREATE TABLE calendar_exposures (
    calendar_id TEXT NOT NULL REFERENCES calendars(id) ON DELETE CASCADE,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    exposed BOOLEAN NOT NULL,
    PRIMARY KEY (calendar_id, user_id)
);

-- +goose Down
DROP TABLE calendar_exposures;
