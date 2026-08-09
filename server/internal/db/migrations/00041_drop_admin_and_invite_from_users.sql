-- +goose Up
-- ADR-0044: retires the instance-wide Admin role (ADR-0037) and ADR-0042's
-- account-level Invite/Pending mechanism, both fully superseded by
-- per-Workspace Role and Workspace Invite. Authority over another User no
-- longer exists anywhere on this instance; account lifecycle (Disable,
-- Delete) becomes self-service only (AccountService).
DROP INDEX idx_users_invite_token_hash;
ALTER TABLE users DROP COLUMN is_admin;
ALTER TABLE users DROP COLUMN invite_token_hash;
ALTER TABLE users DROP COLUMN invite_expires_at;

-- +goose Down
ALTER TABLE users ADD COLUMN is_admin INTEGER NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN invite_token_hash TEXT;
ALTER TABLE users ADD COLUMN invite_expires_at TIMESTAMP;
CREATE INDEX idx_users_invite_token_hash ON users(invite_token_hash);
