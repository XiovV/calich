-- +goose Up
-- Busy (#319, ADR-0086): transp holds iCalendar's own TRANSP property
-- (OPAQUE/TRANSPARENT), the field a Booking Link will later read to decide
-- whether an Event closes a slot. Defaults to OPAQUE (Busy) at the column
-- level, matching RFC 5545's own default for a VEVENT with no TRANSP at
-- all — the all-day departure (ADR-0086's "an all-day Event defaults to
-- Free") is applied by the write path that creates one, not by this
-- column, which stays the RFC's default for everything else. Every
-- pre-existing row reads as Busy: backfilling TRANSPARENT onto an existing
-- all-day Event would silently change the meaning of data a User already
-- entered.
ALTER TABLE events ADD COLUMN transp TEXT NOT NULL DEFAULT 'OPAQUE';

-- +goose Down
ALTER TABLE events DROP COLUMN transp;
