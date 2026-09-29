-- +goose Up
-- handle is a Handle (#321, ADR-0084): a User's public name, the first
-- identifier in this app safe to show a stranger. Nullable — a User who
-- never publishes anything has no Handle (ADR-0084's "seeding one for every
-- existing User at migration time was rejected") — and COLLATE NOCASE,
-- unique on the same case-insensitive terms as email (ADR-0047), except
-- SQLite's ALTER TABLE can't add a UNIQUE column directly, hence the
-- separate index below. Several NULLs are allowed by a UNIQUE index, so
-- every User with no Handle coexists without conflict.
ALTER TABLE users ADD COLUMN handle TEXT COLLATE NOCASE;
CREATE UNIQUE INDEX idx_users_handle ON users(handle);

-- +goose Down
DROP INDEX idx_users_handle;
ALTER TABLE users DROP COLUMN handle;
