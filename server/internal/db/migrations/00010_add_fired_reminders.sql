-- +goose Up
-- The firing engine's exactly-once ledger (ADR-0021): a Reminder+Occurrence
-- pair is recorded here the first time it fires, and never again — the
-- UNIQUE constraint is what makes a repeated tick or a process restart a
-- no-op for something already fired. Reminder rows are wholesale-replaced on
-- Event update (ADR-0020), so an edited Reminder is a new id and starts a
-- fresh fired history; deleting a Reminder's Event cascades this too.
CREATE TABLE fired_reminders (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    reminder_id INTEGER NOT NULL REFERENCES event_reminders(id) ON DELETE CASCADE,
    occurrence_start TIMESTAMP NOT NULL,
    fired_at TIMESTAMP NOT NULL,
    UNIQUE (reminder_id, occurrence_start)
);

-- +goose Down
DROP TABLE fired_reminders;
