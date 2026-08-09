-- +goose Up
-- color is an optional Event color on a Master or Override, in the same
-- arbitrary-hex value space as Calendar color, that wins outright over the
-- Calendar's color when set (ADR-0043). Additive and non-destructive: every
-- pre-existing row keeps it as NULL, meaning "inherit the Calendar's color".
ALTER TABLE events ADD COLUMN color TEXT;

-- +goose Down
ALTER TABLE events DROP COLUMN color;
