-- +goose Up
-- Cancelling a booking (#327, ADR-0087): the visitor's confirmation mail
-- carries a signed cancel link, and cancelling emails the host — neither is
-- an Invitation or a Cancellation in ADR-0059's sense (no METHOD:REQUEST/
-- CANCEL card, no Attendee lifecycle), so both are one new mail method,
-- 'BOOKING_NOTICE', carrying a fully rendered subject/body rather than a
-- typed snapshot InvitationSender interprets — the sender doesn't need to
-- know which of the two it is, only how to relay it.
--
-- SQLite cannot alter a CHECK in place, so this rebuilds the table wholesale
-- (ADR-0048's append-only era), copying every row forward unchanged — the
-- only change is the widened mail method CHECK.
CREATE TABLE outbox_new (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    event_id          TEXT NOT NULL,
    recipient_user_id INTEGER REFERENCES users(id) ON DELETE CASCADE,
    recipient_email   TEXT COLLATE NOCASE,
    actor_user_id     INTEGER REFERENCES users(id) ON DELETE SET NULL,
    kind              TEXT NOT NULL DEFAULT 'mail' CHECK (kind IN ('mail', 'writeback')),
    method            TEXT NOT NULL DEFAULT 'REQUEST' CHECK (method IN ('REQUEST', 'CANCEL', 'BOOKING_NOTICE', 'PATCH', 'POST', 'DELETE', 'INSTANCE', 'CANCEL_INSTANCE')),
    snapshot          TEXT,
    status            TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'sent', 'skipped', 'failed')),
    attempts          INTEGER NOT NULL DEFAULT 0,
    next_attempt_at   TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_error        TEXT,
    created_at        TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    sent_at           TIMESTAMP,
    CHECK (
        (kind = 'mail' AND method IN ('REQUEST', 'CANCEL', 'BOOKING_NOTICE') AND (recipient_user_id IS NULL) <> (recipient_email IS NULL))
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
-- Any 'BOOKING_NOTICE' row is deleted outright, not folded into another
-- method — there is no existing method it means the same thing as.
DELETE FROM outbox WHERE method = 'BOOKING_NOTICE';

CREATE TABLE outbox_old (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    event_id          TEXT NOT NULL,
    recipient_user_id INTEGER REFERENCES users(id) ON DELETE CASCADE,
    recipient_email   TEXT COLLATE NOCASE,
    actor_user_id     INTEGER REFERENCES users(id) ON DELETE SET NULL,
    kind              TEXT NOT NULL DEFAULT 'mail' CHECK (kind IN ('mail', 'writeback')),
    method            TEXT NOT NULL DEFAULT 'REQUEST' CHECK (method IN ('REQUEST', 'CANCEL', 'PATCH', 'POST', 'DELETE', 'INSTANCE', 'CANCEL_INSTANCE')),
    snapshot          TEXT,
    status            TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'sent', 'skipped', 'failed')),
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

INSERT INTO outbox_old (id, event_id, recipient_user_id, recipient_email, actor_user_id, kind, method, snapshot, status, attempts, next_attempt_at, last_error, created_at, sent_at)
SELECT id, event_id, recipient_user_id, recipient_email, actor_user_id, kind, method, snapshot, status, attempts, next_attempt_at, last_error, created_at, sent_at
FROM outbox;

DROP TABLE outbox;
ALTER TABLE outbox_old RENAME TO outbox;

CREATE INDEX idx_outbox_status_id ON outbox(status, id);
CREATE INDEX idx_outbox_actor_created ON outbox(actor_user_id, created_at);
