-- +goose Up
-- ADR-0037: Disable is a reversible account state, not a data operation.
-- Everything the Disabled User owns — Calendars, Events, Shares — stays
-- untouched; only their ability to authenticate stops.
ALTER TABLE users ADD COLUMN is_disabled INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE users DROP COLUMN is_disabled;
