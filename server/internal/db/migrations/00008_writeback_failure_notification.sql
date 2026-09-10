-- +goose Up
-- A Write-back that permanently fails on a writable Linked Calendar raises a
-- Notification, not only the existing per-Event marker and Source
-- needs-attention state (#299, ADR-0081). ADR-0079's own thesis — "a
-- mechanism that reports nothing is indistinguishable from a system with
-- nothing to report" — applies here to a phone: it received success for the
-- local write and has no protocol affordance to be told later the push to
-- Google failed.
--
-- Coalesced on the Source, not the Event: a dead grant fails every one of
-- its queued pushes at once, and one Notification per Event would mean
-- dozens landing for a single underlying cause. So this widens the existing
-- term (ADR-0061) rather than adding a table: kind gains 'writeback_failed',
-- event_id becomes nullable (a writeback_failed Notification concerns a
-- Calendar, not any one Event — occurrence_start already has the same
-- "not every kind needs this" shape), and a new calendar_id column names
-- which one. The partial unique index below on (user_id, calendar_id) WHERE
-- kind = 'writeback_failed' is what performs the coalescing: re-raising the
-- same Source's failure upserts the same row (fresh title, fresh fired_at,
-- seen reset) rather than inserting a second one.
--
-- SQLite cannot alter a CHECK or add a partial unique index to an existing
-- table in place, so this rebuilds the table wholesale (ADR-0048's
-- append-only era), copying every existing row forward unchanged — every one
-- is 'reminder' or 'invite', both keeping their NOT NULL event_id and NULL
-- calendar_id.
CREATE TABLE notifications_new (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    event_id TEXT REFERENCES events(id) ON DELETE CASCADE,
    calendar_id TEXT REFERENCES calendars(id) ON DELETE CASCADE,
    kind TEXT NOT NULL DEFAULT 'reminder' CHECK (kind IN ('reminder', 'invite', 'writeback_failed')),
    occurrence_start TIMESTAMP,
    title TEXT NOT NULL,
    fired_at TIMESTAMP NOT NULL,
    seen BOOLEAN NOT NULL DEFAULT 0,
    CHECK (
        (kind IN ('reminder', 'invite') AND event_id IS NOT NULL AND calendar_id IS NULL)
        OR
        (kind = 'writeback_failed' AND calendar_id IS NOT NULL AND event_id IS NULL)
    )
);

INSERT INTO notifications_new (id, user_id, event_id, kind, occurrence_start, title, fired_at, seen)
SELECT id, user_id, event_id, kind, occurrence_start, title, fired_at, seen
FROM notifications;

DROP TABLE notifications;
ALTER TABLE notifications_new RENAME TO notifications;

CREATE INDEX idx_notifications_user_id ON notifications(user_id, fired_at DESC);
CREATE UNIQUE INDEX idx_notifications_writeback_failure ON notifications(user_id, calendar_id) WHERE kind = 'writeback_failed';

-- +goose Down
-- 'writeback_failed' rows are dropped: the old schema's event_id is NOT
-- NULL and these rows carry none, so there is no lossless way back.
CREATE TABLE notifications_old (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    event_id TEXT NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    kind TEXT NOT NULL DEFAULT 'reminder' CHECK (kind IN ('reminder', 'invite')),
    occurrence_start TIMESTAMP,
    title TEXT NOT NULL,
    fired_at TIMESTAMP NOT NULL,
    seen BOOLEAN NOT NULL DEFAULT 0
);

INSERT INTO notifications_old (id, user_id, event_id, kind, occurrence_start, title, fired_at, seen)
SELECT id, user_id, event_id, kind, occurrence_start, title, fired_at, seen
FROM notifications
WHERE kind IN ('reminder', 'invite');

DROP TABLE notifications;
ALTER TABLE notifications_old RENAME TO notifications;

CREATE INDEX idx_notifications_user_id ON notifications(user_id, fired_at DESC);
