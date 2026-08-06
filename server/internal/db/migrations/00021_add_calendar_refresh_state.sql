-- +goose Up
-- The background poller's scheduling and failure state, kept per Calendar
-- (#86, ADR-0033). next_refresh_at is when the poller should next attempt
-- this Calendar — set at Subscribe time (staggered) and recomputed after
-- every attempt, success or failure. refresh_interval_seconds is the
-- publisher's own stated cadence (RFC 7986 REFRESH-INTERVAL or
-- X-PUBLISHED-TTL), honoured only when longer than the poller's own
-- default. failure_count/error_class/error_message track a broken feed
-- without ever disabling or deleting the Subscription: failure_count drives
-- exponential backoff and resets to 0 on the next success; error_class is
-- "needs_attention" (a human must fix something — bad auth, not found,
-- unparseable feed) or "retrying" (timeout, server error, DNS — expected to
-- clear on its own). All five are NULL/0 until the first scheduling
-- decision or Refresh attempt, and only a Subscribed Calendar ever sets
-- them.
ALTER TABLE calendars ADD COLUMN next_refresh_at TIMESTAMP;
ALTER TABLE calendars ADD COLUMN refresh_interval_seconds INTEGER;
ALTER TABLE calendars ADD COLUMN failure_count INTEGER NOT NULL DEFAULT 0;
ALTER TABLE calendars ADD COLUMN error_class TEXT;
ALTER TABLE calendars ADD COLUMN error_message TEXT;

-- +goose Down
ALTER TABLE calendars DROP COLUMN error_message;
ALTER TABLE calendars DROP COLUMN error_class;
ALTER TABLE calendars DROP COLUMN failure_count;
ALTER TABLE calendars DROP COLUMN refresh_interval_seconds;
ALTER TABLE calendars DROP COLUMN next_refresh_at;
