-- +goose Up
-- Preferences (ADR-0039): per-User display settings the app previously
-- decided by accident — date-fns' default week start, a hardcoded 12-hour
-- format string, no persisted Default view. All five land in one migration
-- even though only week_start is wired up yet (#128); #129, #130, #131 use
-- the rest.
--
-- The default of 1 (Monday) for week_start and '24h' for time_format change
-- what every existing User sees on upgrade. That is deliberate — see
-- ADR-0039 — and not a "preserve current behaviour" default of 0 / '12h'.
ALTER TABLE users ADD COLUMN week_start          INTEGER NOT NULL DEFAULT 1;
ALTER TABLE users ADD COLUMN default_view        TEXT    NOT NULL DEFAULT 'week';
ALTER TABLE users ADD COLUMN time_format         TEXT    NOT NULL DEFAULT '24h';
ALTER TABLE users ADD COLUMN working_hours_start INTEGER NULL;
ALTER TABLE users ADD COLUMN working_hours_end   INTEGER NULL;

-- +goose Down
ALTER TABLE users DROP COLUMN week_start;
ALTER TABLE users DROP COLUMN default_view;
ALTER TABLE users DROP COLUMN time_format;
ALTER TABLE users DROP COLUMN working_hours_start;
ALTER TABLE users DROP COLUMN working_hours_end;
