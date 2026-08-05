-- +goose Up
-- Per-user toggle resolving the CalDAV double-fire question (ADR-0027): when
-- on, the reminder scheduler skips the Notification channel because the
-- user's synced devices already show their own pop-ups from the synced
-- VALARM. Default off so web-only users are unaffected. The Email channel is
-- never gated by this — no device competes for it (ADR-0027).
ALTER TABLE users ADD COLUMN synced_device_reminders_enabled INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE users DROP COLUMN synced_device_reminders_enabled;
