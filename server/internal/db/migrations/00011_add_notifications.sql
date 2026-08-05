-- +goose Up
-- A Notification is the persistent in-app record created when a
-- Notification-Channel Reminder fires (ADR-0021, CONTEXT.md). Title is
-- denormalized (copied at fire time) so a Notification keeps reading
-- correctly even if the Event's title later changes.
CREATE TABLE notifications (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    event_id TEXT NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    occurrence_start TIMESTAMP NOT NULL,
    title TEXT NOT NULL,
    fired_at TIMESTAMP NOT NULL,
    seen BOOLEAN NOT NULL DEFAULT 0
);

CREATE INDEX idx_notifications_user_id ON notifications(user_id, fired_at DESC);

-- +goose Down
DROP TABLE notifications;
