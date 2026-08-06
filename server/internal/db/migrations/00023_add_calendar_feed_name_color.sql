-- +goose Up
-- feed_name/feed_color are a Subscribed Calendar's shadow columns (#88,
-- ADR-0032): the last value the feed itself supplied (X-WR-CALNAME /
-- X-APPLE-CALENDAR-COLOR), as opposed to name/color, which are what's
-- actually displayed. A Refresh updates the displayed value only while it
-- still equals its shadow — that comparison alone is the "overridden by the
-- User" flag, with no separate column for it. Only a Subscribed Calendar
-- ever sets them; NULL means the feed has never supplied one (or the
-- Calendar isn't subscribed at all).
ALTER TABLE calendars ADD COLUMN feed_name TEXT;
ALTER TABLE calendars ADD COLUMN feed_color TEXT;

-- +goose Down
ALTER TABLE calendars DROP COLUMN feed_color;
ALTER TABLE calendars DROP COLUMN feed_name;
