-- +goose Up
-- Sharing (ADR-0034) means a Reminder now fans out to every User with
-- Access to its Event's Calendar (ADR-0036), and different Users can fire
-- at different offsets for the same Reminder and Occurrence — the ledger's
-- exactly-once key must include which User fired, not just which Reminder
-- and Occurrence. SQLite can't add a column into an existing UNIQUE
-- constraint, so the table is rebuilt.
CREATE TABLE fired_reminders_new (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    reminder_id INTEGER NOT NULL REFERENCES event_reminders(id) ON DELETE CASCADE,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    occurrence_start TIMESTAMP NOT NULL,
    fired_at TIMESTAMP NOT NULL,
    UNIQUE (reminder_id, occurrence_start, user_id)
);

-- No backfill: existing rows predate multi-user fan-out and every one of
-- them fired for the sole Owner of a single-user instance, so there is no
-- User a row could be attributed to other than that Calendar's Owner.
INSERT INTO fired_reminders_new (id, reminder_id, user_id, occurrence_start, fired_at)
SELECT fr.id, fr.reminder_id, c.user_id, fr.occurrence_start, fr.fired_at
FROM fired_reminders fr
JOIN event_reminders er ON er.id = fr.reminder_id
JOIN events e ON e.id = er.event_id
JOIN calendars c ON c.id = e.calendar_id;

DROP TABLE fired_reminders;
ALTER TABLE fired_reminders_new RENAME TO fired_reminders;

-- +goose Down
CREATE TABLE fired_reminders_old (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    reminder_id INTEGER NOT NULL REFERENCES event_reminders(id) ON DELETE CASCADE,
    occurrence_start TIMESTAMP NOT NULL,
    fired_at TIMESTAMP NOT NULL,
    UNIQUE (reminder_id, occurrence_start)
);
INSERT INTO fired_reminders_old (id, reminder_id, occurrence_start, fired_at)
SELECT MIN(id), reminder_id, occurrence_start, fired_at FROM fired_reminders GROUP BY reminder_id, occurrence_start;
DROP TABLE fired_reminders;
ALTER TABLE fired_reminders_old RENAME TO fired_reminders;
