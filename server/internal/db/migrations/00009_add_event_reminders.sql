-- +goose Up
-- A Reminder is a trigger offset (minutes before Occurrence start) plus a
-- delivery channel, projecting an iCalendar VALARM (ADR-0020). Lives on the
-- Event row in a child table, not a JSON blob, so the phase-2 scheduler
-- (ADR-0021) can query it. Many per Event, unconstrained: no cap, no dedupe,
-- so no unique constraint on (event_id, offset_minutes, channel).
CREATE TABLE event_reminders (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    event_id TEXT NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    offset_minutes INTEGER NOT NULL,
    channel TEXT NOT NULL
);

CREATE INDEX idx_event_reminders_event_id ON event_reminders(event_id);

-- +goose Down
DROP TABLE event_reminders;
