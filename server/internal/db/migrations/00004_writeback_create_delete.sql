-- +goose Up
-- Create and delete Write-back (#292, ADR-0075, ADR-0076, ADR-0077): creating
-- and deleting an Event on a writable Linked Calendar now pushes to the
-- Provider too, alongside #290's edit push. Both go through the same outbox
-- as the PATCH already does, so the background Worker's shape is unchanged.
--
-- 'POST' and 'DELETE' join 'PATCH' in the method CHECK for a 'writeback' row:
--   - POST is events.insert — a locally created Event with no ExternalUID yet.
--     SendWriteBack issues it, then adopts the id Google returns onto the row
--     (EventRepository.AdoptProviderIdentity), so the next Refresh reconciles
--     the Event by that id rather than tombstoning it as absent (ADR-0076).
--   - DELETE is events.delete — a Master removed here. The local row is gone
--     by the time the push drains, so unlike POST/PATCH this row cannot
--     rebuild from live state: it carries a snapshot (the Calendar's id and
--     the Event's ExternalUID) in the existing `snapshot` column, exactly as
--     a mail CANCEL already does for the same reason.
--
-- SQLite cannot alter a CHECK in place, so this rebuilds the table wholesale
-- (ADR-0048's append-only era), copying every row forward unchanged — the
-- only change is the widened method CHECK.
CREATE TABLE outbox_new (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    event_id          TEXT NOT NULL,
    recipient_user_id INTEGER REFERENCES users(id) ON DELETE CASCADE,
    recipient_email   TEXT COLLATE NOCASE,
    actor_user_id     INTEGER REFERENCES users(id) ON DELETE SET NULL,
    kind              TEXT NOT NULL DEFAULT 'mail' CHECK (kind IN ('mail', 'writeback')),
    method            TEXT NOT NULL DEFAULT 'REQUEST' CHECK (method IN ('REQUEST', 'CANCEL', 'PATCH', 'POST', 'DELETE')),
    snapshot          TEXT,
    status            TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'sent', 'failed')),
    attempts          INTEGER NOT NULL DEFAULT 0,
    next_attempt_at   TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_error        TEXT,
    created_at        TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    sent_at           TIMESTAMP,
    CHECK (
        (kind = 'mail' AND method IN ('REQUEST', 'CANCEL') AND (recipient_user_id IS NULL) <> (recipient_email IS NULL))
        OR
        (kind = 'writeback' AND method IN ('PATCH', 'POST', 'DELETE') AND recipient_user_id IS NULL AND recipient_email IS NULL)
    )
);

INSERT INTO outbox_new (id, event_id, recipient_user_id, recipient_email, actor_user_id, kind, method, snapshot, status, attempts, next_attempt_at, last_error, created_at, sent_at)
SELECT id, event_id, recipient_user_id, recipient_email, actor_user_id, kind, method, snapshot, status, attempts, next_attempt_at, last_error, created_at, sent_at
FROM outbox;

DROP TABLE outbox;
ALTER TABLE outbox_new RENAME TO outbox;

CREATE INDEX idx_outbox_status_id ON outbox(status, id);
CREATE INDEX idx_outbox_actor_created ON outbox(actor_user_id, created_at);

-- +goose Down
CREATE TABLE outbox_old (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    event_id          TEXT NOT NULL,
    recipient_user_id INTEGER REFERENCES users(id) ON DELETE CASCADE,
    recipient_email   TEXT COLLATE NOCASE,
    actor_user_id     INTEGER REFERENCES users(id) ON DELETE SET NULL,
    kind              TEXT NOT NULL DEFAULT 'mail' CHECK (kind IN ('mail', 'writeback')),
    method            TEXT NOT NULL DEFAULT 'REQUEST' CHECK (method IN ('REQUEST', 'CANCEL', 'PATCH')),
    snapshot          TEXT,
    status            TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'sent', 'failed')),
    attempts          INTEGER NOT NULL DEFAULT 0,
    next_attempt_at   TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_error        TEXT,
    created_at        TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    sent_at           TIMESTAMP,
    CHECK (
        (kind = 'mail' AND method IN ('REQUEST', 'CANCEL') AND (recipient_user_id IS NULL) <> (recipient_email IS NULL))
        OR
        (kind = 'writeback' AND method = 'PATCH' AND recipient_user_id IS NULL AND recipient_email IS NULL)
    )
);

INSERT INTO outbox_old (id, event_id, recipient_user_id, recipient_email, actor_user_id, kind, method, snapshot, status, attempts, next_attempt_at, last_error, created_at, sent_at)
SELECT id, event_id, recipient_user_id, recipient_email, actor_user_id, kind, method, snapshot, status, attempts, next_attempt_at, last_error, created_at, sent_at
FROM outbox
WHERE method IN ('REQUEST', 'CANCEL', 'PATCH');

DROP TABLE outbox;
ALTER TABLE outbox_old RENAME TO outbox;

CREATE INDEX idx_outbox_status_id ON outbox(status, id);
CREATE INDEX idx_outbox_actor_created ON outbox(actor_user_id, created_at);
