-- +goose Up
-- A Write-back that was never dispatched is 'skipped', not 'sent' (ADR-0079).
--
-- SendWriteBack has several "nothing left to push" paths that end a message
-- without ever calling Google: the Event was deleted after the push was queued,
-- the Calendar stopped being a writable Linked Calendar, its Connection was
-- removed, or a PATCH turned out to name an Event that never reached the
-- Provider at all. Each returned a bare nil, which the Worker recorded as
-- 'sent' — a claim of delivery attached to a request that never left the
-- building, sitting beside whatever last_error a previous attempt had left.
--
-- That made the one question worth asking of this table — "did my edit actually
-- reach Google?" — unanswerable from the row. The four statuses now mean:
--
--   pending  queued, or waiting out a backoff
--   sent     dispatched, and Google accepted it
--   skipped  terminal; never dispatched, with the reason in last_error
--   failed   dispatched, and permanently rejected
--
-- 'skipped' is terminal like 'sent': the Worker does not retry it, because the
-- state that produced it will not resolve on its own. Unlike 'failed' it raises
-- no per-Event marker of its own — most skips are races the User caused
-- (deleting the Event, disconnecting the account), and the ones that are not
-- have already raised the Source's needs-attention state before returning.
--
-- Existing rows are left alone. A historical 'sent' that was really a skip is
-- indistinguishable now, and inventing a reason for it would be worse than
-- leaving it: from here on the distinction is recorded, and before here it was
-- not.
--
-- SQLite cannot alter a CHECK in place, so this rebuilds the table wholesale
-- (ADR-0048's append-only era), copying every row forward unchanged — the only
-- change is the widened status CHECK.
CREATE TABLE outbox_new (
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

INSERT INTO outbox_new (id, event_id, recipient_user_id, recipient_email, actor_user_id, kind, method, snapshot, status, attempts, next_attempt_at, last_error, created_at, sent_at)
SELECT id, event_id, recipient_user_id, recipient_email, actor_user_id, kind, method, snapshot, status, attempts, next_attempt_at, last_error, created_at, sent_at
FROM outbox;

DROP TABLE outbox;
ALTER TABLE outbox_new RENAME TO outbox;

CREATE INDEX idx_outbox_status_id ON outbox(status, id);
CREATE INDEX idx_outbox_actor_created ON outbox(actor_user_id, created_at);

-- +goose Down
-- 'skipped' rows fold back into 'sent', the status they carried before this
-- migration existed. The reason survives in last_error; only the distinction is
-- lost, which is precisely what the Up half added.
CREATE TABLE outbox_old (
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

INSERT INTO outbox_old (id, event_id, recipient_user_id, recipient_email, actor_user_id, kind, method, snapshot, status, attempts, next_attempt_at, last_error, created_at, sent_at)
SELECT id, event_id, recipient_user_id, recipient_email, actor_user_id, kind, method, snapshot,
       CASE status WHEN 'skipped' THEN 'sent' ELSE status END,
       attempts, next_attempt_at, last_error, created_at, sent_at
FROM outbox;

DROP TABLE outbox;
ALTER TABLE outbox_old RENAME TO outbox;

CREATE INDEX idx_outbox_status_id ON outbox(status, id);
CREATE INDEX idx_outbox_actor_created ON outbox(actor_user_id, created_at);
