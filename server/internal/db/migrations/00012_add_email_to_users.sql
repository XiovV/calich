-- +goose Up
-- A per-user email address (nullable) — the Email-Channel Reminder's
-- recipient (ADR-0021). Per-user, not a global env var, per ADR-0010's
-- refusal to bake in a singleton user.
ALTER TABLE users ADD COLUMN email TEXT;

-- +goose Down
ALTER TABLE users DROP COLUMN email;
