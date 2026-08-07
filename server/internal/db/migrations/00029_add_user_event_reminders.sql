-- +goose Up
-- user_event_reminders is the Reminder override (ADR-0036, CONTEXT.md): one
-- User's personal replacement for an Event's Reminders — a different
-- offset, a different Channel, or muted entirely — applying to that User
-- alone. Keyed on (user_id, event_id) rather than the Reminder, because
-- event_reminders is wholesale-replaced on every Event update (ADR-0020); a
-- Reminder-keyed override would silently vanish on the next unrelated edit,
-- an Event-keyed one survives it.
--
-- offset_minutes and channel are independently nullable so a User can
-- override just one of them and keep the Event's own value for the other;
-- muted, when set, mutes every Reminder on the Event for this User
-- regardless of what offset_minutes/channel hold.
--
-- Never projected into iCalendar (ADR-0036) — CalDAV keeps serving
-- event_reminders unchanged and byte-identical to every principal.
CREATE TABLE user_event_reminders (
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    event_id TEXT NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    offset_minutes INTEGER,
    channel TEXT CHECK (channel IS NULL OR channel IN ('notification', 'email')),
    muted BOOLEAN NOT NULL DEFAULT 0,
    PRIMARY KEY (user_id, event_id)
);

-- +goose Down
DROP TABLE user_event_reminders;
