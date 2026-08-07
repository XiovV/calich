-- +goose Up
-- source_url makes a Calendar a Subscribed Calendar (#83, ADR-0032): a
-- non-null value binds it to an external .ics feed. external_uid is the
-- foreign iCalendar UID a Refresh will later reconcile against (ADR-0033),
-- stored alongside the Event rather than as its id so the row id stays a
-- minted UUID. Both are additive and non-destructive: every pre-existing
-- row keeps NULL, meaning an ordinary Calendar/Event.
ALTER TABLE calendars ADD COLUMN source_url TEXT;
ALTER TABLE events ADD COLUMN external_uid TEXT;

-- Unique per Calendar, and only enforced on Masters (parent_id IS NULL):
-- an Override legitimately shares its Master's iCalendar UID.
CREATE UNIQUE INDEX idx_events_calendar_external_uid ON events(calendar_id, external_uid)
  WHERE external_uid IS NOT NULL AND parent_id IS NULL;

-- +goose Down
DROP INDEX idx_events_calendar_external_uid;
ALTER TABLE events DROP COLUMN external_uid;
ALTER TABLE calendars DROP COLUMN source_url;
