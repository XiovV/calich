-- +goose Up
-- rrule holds the event's iCalendar RRULE (RFC 5545) as opaque text. Empty for a
-- non-recurring event. The backend stays a thin CRUD store and does not expand
-- recurrence; the web frontend expands occurrences over the visible window
-- (ADR-0016).
ALTER TABLE events ADD COLUMN rrule TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE events DROP COLUMN rrule;
