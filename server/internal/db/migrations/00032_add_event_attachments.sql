-- +goose Up
-- Attachments (#132, ADR-0040): a file uploaded to this instance and held
-- against a Master, shown on every Occurrence of its series. An Override
-- cannot carry one of its own — enforced in the service layer (a Master's
-- id is required), not by a constraint here, since SQLite has no partial
-- foreign key.
--
-- id is also the filename on disk (DATA_DIR/attachments/<id[:2]>/<id>) and
-- the RFC 8607 MANAGED-ID; minted server-side only, never accepted from a
-- client. filename is the user's own name for the file, used only in
-- Content-Disposition — nothing user-supplied ever reaches the filesystem.
--
-- uploaded_by is attribution only, mirroring events.created_by
-- (00024_drop_events_user_id.sql): never consulted for authorization, which
-- resolves through the Event's Calendar instead (ADR-0034).
CREATE TABLE event_attachments (
    id           TEXT PRIMARY KEY,
    event_id     TEXT NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    filename     TEXT NOT NULL,
    content_type TEXT NOT NULL,
    size_bytes   INTEGER NOT NULL,
    uploaded_by  INTEGER REFERENCES users(id) ON DELETE SET NULL,
    created_at   TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_event_attachments_event_id ON event_attachments(event_id);

-- +goose Down
DROP INDEX idx_event_attachments_event_id;
DROP TABLE event_attachments;
