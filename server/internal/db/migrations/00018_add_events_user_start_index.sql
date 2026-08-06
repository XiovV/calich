-- +goose Up
-- Supports ListByUser's windowed query (#80): filters on user_id plus
-- "start", and always orders by "start" — idx_events_user_id alone can't
-- serve either.
CREATE INDEX idx_events_user_start ON events(user_id, "start");

-- +goose Down
DROP INDEX idx_events_user_start;
