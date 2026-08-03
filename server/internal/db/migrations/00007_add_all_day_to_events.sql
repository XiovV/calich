-- +goose Up
-- all_day flags an Event as occupying whole dates rather than a time range
-- (ADR-0017). It reuses the existing start/end columns, storing the
-- half-open date range (start = the date, end = the exclusive next day).
ALTER TABLE events ADD COLUMN all_day INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE events DROP COLUMN all_day;
