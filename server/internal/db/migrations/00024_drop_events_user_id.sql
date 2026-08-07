-- +goose Up
-- events.user_id used to answer four unrelated questions at once
-- (authorization, reminder recipient, cascade delete, CalDAV home-set), and
-- they only agreed because there was exactly one user. Under Calendar
-- sharing they don't: an Event has no owner of its own, it inherits Access
-- from its Calendar (ADR-0034). Dropping the column forces each of those
-- four jobs to be answered separately and explicitly.
--
-- The cascade chain survives untouched: events.calendar_id already has ON
-- DELETE CASCADE, so deleting a User still removes their Calendars and
-- therefore their Events.
DROP INDEX idx_events_user_id;
DROP INDEX idx_events_user_start;
ALTER TABLE events DROP COLUMN user_id;

-- created_by is attribution only — who created the Event — and is
-- deliberately never consulted for authorization, so it can never be
-- mistaken for an access concept. SET NULL rather than CASCADE: a departing
-- household member's Events must not vanish from a Calendar they did not
-- own.
ALTER TABLE events ADD COLUMN created_by INTEGER REFERENCES users(id) ON DELETE SET NULL;

-- Replaces idx_events_user_start: ListByCalendarIDs now windows on
-- calendar_id instead of the dropped user_id, ordering by "start" the same
-- way (#80).
CREATE INDEX idx_events_calendar_start ON events(calendar_id, "start");

-- +goose Down
DROP INDEX idx_events_calendar_start;
ALTER TABLE events DROP COLUMN created_by;
ALTER TABLE events ADD COLUMN user_id INTEGER REFERENCES users(id) ON DELETE CASCADE;
CREATE INDEX idx_events_user_id ON events(user_id);
CREATE INDEX idx_events_user_start ON events(user_id, "start");
