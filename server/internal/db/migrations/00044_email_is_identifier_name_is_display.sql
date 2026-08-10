-- +goose Up
-- ADR-0047: Email becomes the account's unique login identifier (web
-- sign-in, CalDAV Basic auth, Share-target resolution); username becomes
-- Name, a non-unique display label. SQLite can't add/drop a UNIQUE
-- constraint or a NOT NULL requirement on an existing column in place, so
-- the table is rebuilt (same pattern as 00038's calendars rebuild).
--
-- This is deliberately fail-loud: if any existing row has a NULL email or
-- shares an email with another row, the INSERT below hits the new email
-- UNIQUE/NOT NULL constraint and the whole migration transaction rolls
-- back rather than silently inventing a placeholder email for anyone.
CREATE TABLE users_new (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    password_hash TEXT NOT NULL,
    must_change_password INTEGER NOT NULL DEFAULT 0,
    email TEXT NOT NULL UNIQUE,
    synced_device_reminders_enabled INTEGER NOT NULL DEFAULT 0,
    is_disabled INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    week_start INTEGER NOT NULL DEFAULT 1,
    default_view TEXT NOT NULL DEFAULT 'week',
    time_format TEXT NOT NULL DEFAULT '24h',
    working_hours_start INTEGER NULL,
    working_hours_end INTEGER NULL
);

INSERT INTO users_new
SELECT id, username, password_hash, must_change_password, email,
       synced_device_reminders_enabled, is_disabled, created_at,
       week_start, default_view, time_format, working_hours_start, working_hours_end
FROM users;

DROP TABLE users;
ALTER TABLE users_new RENAME TO users;

-- +goose Down
CREATE TABLE users_old (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    username TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    must_change_password INTEGER NOT NULL DEFAULT 0,
    email TEXT,
    synced_device_reminders_enabled INTEGER NOT NULL DEFAULT 0,
    is_disabled INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    week_start INTEGER NOT NULL DEFAULT 1,
    default_view TEXT NOT NULL DEFAULT 'week',
    time_format TEXT NOT NULL DEFAULT '24h',
    working_hours_start INTEGER NULL,
    working_hours_end INTEGER NULL
);

INSERT INTO users_old
SELECT id, name, password_hash, must_change_password, email,
       synced_device_reminders_enabled, is_disabled, created_at,
       week_start, default_view, time_format, working_hours_start, working_hours_end
FROM users;

DROP TABLE users;
ALTER TABLE users_old RENAME TO users;
