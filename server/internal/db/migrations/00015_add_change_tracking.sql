-- +goose Up
CREATE TABLE change_sequence (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    value INTEGER NOT NULL
);
INSERT INTO change_sequence (id, value) VALUES (1, 0);

ALTER TABLE events ADD COLUMN change_seq INTEGER NOT NULL DEFAULT 0;
CREATE INDEX idx_events_calendar_change_seq ON events(calendar_id, change_seq);

CREATE TABLE deleted_objects (
    calendar_id TEXT NOT NULL REFERENCES calendars(id) ON DELETE CASCADE,
    uid TEXT NOT NULL,
    change_seq INTEGER NOT NULL
);
CREATE INDEX idx_deleted_objects_calendar_change_seq ON deleted_objects(calendar_id, change_seq);

-- +goose Down
DROP TABLE deleted_objects;
DROP INDEX idx_events_calendar_change_seq;
ALTER TABLE events DROP COLUMN change_seq;
DROP TABLE change_sequence;
