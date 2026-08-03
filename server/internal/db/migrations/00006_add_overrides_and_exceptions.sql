-- +goose Up
-- parent_id + recurrence_id turn a row into an Override: a complete standalone
-- Event that replaces one Occurrence of its parent's series, keyed by the
-- Occurrence it replaces (its RECURRENCE-ID). Both are null on a Master
-- (recurring or not). ON DELETE CASCADE means deleting a series auto-cleans
-- its overrides (ADR-0016).
ALTER TABLE events ADD COLUMN parent_id TEXT REFERENCES events(id) ON DELETE CASCADE;
ALTER TABLE events ADD COLUMN recurrence_id TIMESTAMP;

CREATE INDEX idx_events_parent_id ON events(parent_id);

-- A cancelled single Occurrence (EXDATE) — the rule still generates that slot,
-- but it is suppressed from expansion. ON DELETE CASCADE cleans these up when
-- their series is deleted.
CREATE TABLE event_exceptions (
    parent_id TEXT NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    occurrence_start TIMESTAMP NOT NULL,
    PRIMARY KEY (parent_id, occurrence_start)
);

-- +goose Down
DROP TABLE event_exceptions;
DROP INDEX idx_events_parent_id;
ALTER TABLE events DROP COLUMN recurrence_id;
ALTER TABLE events DROP COLUMN parent_id;
