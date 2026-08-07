-- +goose Up
-- keep_alarms is a per-Subscription setting, off by default (#87,
-- ADR-0032): when false, a Refresh drops the feed's VALARMs exactly as
-- Subscribe's initial import already does; when true, they become
-- Reminders on both Channels, matching ICS import's behaviour. Only a
-- Subscribed Calendar (source_url set) ever sets it to true.
ALTER TABLE calendars ADD COLUMN keep_alarms BOOLEAN NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE calendars DROP COLUMN keep_alarms;
