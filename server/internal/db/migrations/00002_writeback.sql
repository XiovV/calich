-- +goose Up
-- Write-back (#290, ADR-0075, ADR-0076): editing an Event on a writable
-- Linked Calendar pushes a field-scoped PATCH to the Provider. The push is
-- queued through the same outbox mail already uses (ADR-0060), rather than a
-- table of its own, so the background worker's shape — list pending, try,
-- back off, mark sent/failed — serves both without duplicating it.
--
-- kind discriminates what a row asks the worker to do: 'mail' is every
-- existing row (an Invitation or Cancellation, ADR-0059), the default so the
-- migration needs no backfill; 'writeback' is a queued Provider PATCH. The
-- two carry different shapes, which is why the CHECK below is keyed on kind
-- rather than left as one constraint: a 'mail' row still requires exactly one
-- of recipient_user_id/recipient_email (nobody to notify otherwise), while a
-- 'writeback' row addresses no recipient at all — event_id plus the Event's
-- own stored Provider state (external_uid, provider_etag) is everything
-- ConnectionService.SendWriteBack needs to rebuild the push at send time, the
-- same "never a snapshot" contract sendInvitation already keeps (see
-- service/connection_writeback.go).
--
-- 'PATCH' joins REQUEST/CANCEL in the method CHECK — the write-back method
-- ADR-0075 requires be the only shape ever expressible at the Provider seam.
--
-- SQLite cannot alter a CHECK constraint in place, so this rebuilds the table
-- wholesale (ADR-0048's append-only era: this is a new migration file, never
-- an edit to 00001_init.sql) — create the replacement, copy every row
-- forward with kind defaulted to 'mail', drop the original, rename, and
-- recreate its indexes verbatim.
CREATE TABLE outbox_new (
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

INSERT INTO outbox_new (id, event_id, recipient_user_id, recipient_email, actor_user_id, kind, method, snapshot, status, attempts, next_attempt_at, last_error, created_at, sent_at)
SELECT id, event_id, recipient_user_id, recipient_email, actor_user_id, 'mail', method, snapshot, status, attempts, next_attempt_at, last_error, created_at, sent_at
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
    method            TEXT NOT NULL DEFAULT 'REQUEST' CHECK (method IN ('REQUEST', 'CANCEL')),
    snapshot          TEXT,
    status            TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'sent', 'failed')),
    attempts          INTEGER NOT NULL DEFAULT 0,
    next_attempt_at   TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_error        TEXT,
    created_at        TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    sent_at           TIMESTAMP,
    CHECK ((recipient_user_id IS NULL) <> (recipient_email IS NULL))
);

INSERT INTO outbox_old (id, event_id, recipient_user_id, recipient_email, actor_user_id, method, snapshot, status, attempts, next_attempt_at, last_error, created_at, sent_at)
SELECT id, event_id, recipient_user_id, recipient_email, actor_user_id, method, snapshot, status, attempts, next_attempt_at, last_error, created_at, sent_at
FROM outbox
WHERE kind = 'mail';

DROP TABLE outbox;
ALTER TABLE outbox_old RENAME TO outbox;

CREATE INDEX idx_outbox_status_id ON outbox(status, id);
CREATE INDEX idx_outbox_actor_created ON outbox(actor_user_id, created_at);
