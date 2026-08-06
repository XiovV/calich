-- +goose Up
-- Refresh's conditional-GET and no-op-detection state, kept per Calendar
-- (ADR-0033). etag/last_modified mirror the feed's own response validators
-- when it sends them; content_hash is the fallback for a publisher that
-- sends neither, so a feed with no validators still costs a parse only when
-- its body actually changed. last_synced_at is the last time a Refresh
-- completed successfully (with or without any change), shown on the
-- Calendar's row. All four are NULL until the first Refresh runs; only a
-- Subscribed Calendar ever sets them.
ALTER TABLE calendars ADD COLUMN last_synced_at TIMESTAMP;
ALTER TABLE calendars ADD COLUMN etag TEXT;
ALTER TABLE calendars ADD COLUMN last_modified TEXT;
ALTER TABLE calendars ADD COLUMN content_hash TEXT;

-- +goose Down
ALTER TABLE calendars DROP COLUMN content_hash;
ALTER TABLE calendars DROP COLUMN last_modified;
ALTER TABLE calendars DROP COLUMN etag;
ALTER TABLE calendars DROP COLUMN last_synced_at;
