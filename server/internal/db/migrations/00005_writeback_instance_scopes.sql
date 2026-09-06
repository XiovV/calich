-- +goose Up
-- Recurring Write-back: the three edit scopes (#293, ADR-0075, ADR-0078).
-- Editing or deleting one Occurrence of a recurring series on a writable Linked
-- Calendar now pushes to the Provider too, alongside #290's Master edit and
-- #292's whole-series create/delete.
--
-- Two methods join 'PATCH'/'POST'/'DELETE' in the CHECK for a 'writeback' row:
--   - INSTANCE is events.patch against one recurring instance — "This event"
--     editing an Occurrence (a new or existing Override). SendWriteBack resolves
--     the instance's Provider id from events.instances (ADR-0078: fetched, never
--     constructed), then PATCHes it with the field-scoped body.
--   - CANCEL_INSTANCE is events.patch { status: 'cancelled' } against one
--     recurring instance — "Delete this event" (AddException, or deleting an
--     existing Override). Google's native cancellation, deliberately never an
--     EXDATE line in the recurrence array (ADR-0075).
--
-- Both carry an OutboxWriteBackInstanceSnapshot in the existing `snapshot`
-- column (the Calendar id, the Master's local id, the Occurrence's original
-- start, and the Override's local id when there is one). Their `event_id` column
-- holds the *Master's* local id, so the reconciler's existing pending-set
-- protection (keyed on MasterID) covers the whole series while the instance push
-- is in flight, unchanged (ADR-0076, ADR-0078).
--
-- SQLite cannot alter a CHECK in place, so this rebuilds the table wholesale
-- (ADR-0048's append-only era), copying every row forward unchanged — the only
-- change is the widened method CHECK.
CREATE TABLE outbox_new (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    event_id          TEXT NOT NULL,
    recipient_user_id INTEGER REFERENCES users(id) ON DELETE CASCADE,
    recipient_email   TEXT COLLATE NOCASE,
    actor_user_id     INTEGER REFERENCES users(id) ON DELETE SET NULL,
    kind              TEXT NOT NULL DEFAULT 'mail' CHECK (kind IN ('mail', 'writeback')),
    method            TEXT NOT NULL DEFAULT 'REQUEST' CHECK (method IN ('REQUEST', 'CANCEL', 'PATCH', 'POST', 'DELETE', 'INSTANCE', 'CANCEL_INSTANCE')),
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
        (kind = 'writeback' AND method IN ('PATCH', 'POST', 'DELETE', 'INSTANCE', 'CANCEL_INSTANCE') AND recipient_user_id IS NULL AND recipient_email IS NULL)
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

INSERT INTO outbox_old (id, event_id, recipient_user_id, recipient_email, actor_user_id, kind, method, snapshot, status, attempts, next_attempt_at, last_error, created_at, sent_at)
SELECT id, event_id, recipient_user_id, recipient_email, actor_user_id, kind, method, snapshot, status, attempts, next_attempt_at, last_error, created_at, sent_at
FROM outbox
WHERE method IN ('REQUEST', 'CANCEL', 'PATCH', 'POST', 'DELETE');

DROP TABLE outbox;
ALTER TABLE outbox_old RENAME TO outbox;

CREATE INDEX idx_outbox_status_id ON outbox(status, id);
CREATE INDEX idx_outbox_actor_created ON outbox(actor_user_id, created_at);
