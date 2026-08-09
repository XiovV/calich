-- +goose Up
-- ADR-0042: an Invite is a single-use, hashed token (mirroring
-- password_hash) plus its expiry, stored on the row it invites — a Pending
-- User is a User row from the moment it's created, not a separate table.
ALTER TABLE users ADD COLUMN invite_token_hash TEXT;
ALTER TABLE users ADD COLUMN invite_expires_at TIMESTAMP;

CREATE INDEX idx_users_invite_token_hash ON users(invite_token_hash);

-- +goose Down
DROP INDEX idx_users_invite_token_hash;
ALTER TABLE users DROP COLUMN invite_token_hash;
ALTER TABLE users DROP COLUMN invite_expires_at;
