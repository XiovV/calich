-- +goose Up
-- tzid anchors an Event's wall-clock to an IANA zone (a named zone), an
-- absolute instant ("Etc/UTC"), or a Floating Event (NULL) — see ADR-0019.
-- Additive and non-destructive: no existing start/end value is touched, and
-- every pre-existing row keeps tzid = NULL (Floating), preserving its current
-- render/expansion behavior exactly.
ALTER TABLE events ADD COLUMN tzid TEXT;

-- +goose Down
ALTER TABLE events DROP COLUMN tzid;
