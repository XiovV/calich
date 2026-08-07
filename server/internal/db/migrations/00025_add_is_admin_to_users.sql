-- +goose Up
-- ADR-0037: multiple accounts need someone empowered to create them. An
-- Admin's authority is over who exists, never over what they can see — data
-- access still goes through Access (ADR-0034) like everyone else's.
ALTER TABLE users ADD COLUMN is_admin INTEGER NOT NULL DEFAULT 0;

-- The bootstrapped account (ADR-0010) becomes the first Admin, including on
-- an instance upgrading from before this migration existed — otherwise an
-- existing single-user instance would have no Admin at all.
UPDATE users SET is_admin = 1 WHERE id = (SELECT MIN(id) FROM users);

-- +goose Down
ALTER TABLE users DROP COLUMN is_admin;
