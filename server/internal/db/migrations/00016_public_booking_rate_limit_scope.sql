-- +goose Up
-- Widens auth_rate_limit_attempts.scope to admit 'public_booking' (#324,
-- ADR-0087): the public Booking Link page's own per-IP ceiling, throttled
-- the same way Register's is — IP-only, charged on every call.
--
-- SQLite cannot alter a CHECK in place, so this rebuilds the table wholesale
-- (ADR-0048's append-only era), copying every row forward unchanged — the
-- only change is the widened scope CHECK.
CREATE TABLE auth_rate_limit_attempts_new (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    scope      TEXT NOT NULL CHECK (scope IN ('auth', 'register', 'public_booking')),
    key_type   TEXT NOT NULL CHECK (key_type IN ('email', 'ip')),
    key_value  TEXT NOT NULL COLLATE NOCASE,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

INSERT INTO auth_rate_limit_attempts_new (id, scope, key_type, key_value, created_at)
SELECT id, scope, key_type, key_value, created_at
FROM auth_rate_limit_attempts;

DROP TABLE auth_rate_limit_attempts;
ALTER TABLE auth_rate_limit_attempts_new RENAME TO auth_rate_limit_attempts;

CREATE INDEX idx_auth_rate_limit_lookup ON auth_rate_limit_attempts(scope, key_type, key_value, created_at);

-- +goose Down
-- Any 'public_booking' row is deleted outright, not folded into another
-- scope — there is no existing scope it means the same thing as.
DELETE FROM auth_rate_limit_attempts WHERE scope = 'public_booking';

CREATE TABLE auth_rate_limit_attempts_old (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    scope      TEXT NOT NULL CHECK (scope IN ('auth', 'register')),
    key_type   TEXT NOT NULL CHECK (key_type IN ('email', 'ip')),
    key_value  TEXT NOT NULL COLLATE NOCASE,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

INSERT INTO auth_rate_limit_attempts_old (id, scope, key_type, key_value, created_at)
SELECT id, scope, key_type, key_value, created_at
FROM auth_rate_limit_attempts;

DROP TABLE auth_rate_limit_attempts;
ALTER TABLE auth_rate_limit_attempts_old RENAME TO auth_rate_limit_attempts;

CREATE INDEX idx_auth_rate_limit_lookup ON auth_rate_limit_attempts(scope, key_type, key_value, created_at);
