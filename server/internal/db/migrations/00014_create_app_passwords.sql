-- +goose Up
-- App passwords (ADR-0024, #62): per-User credentials for native CalDAV
-- clients, which speak HTTP Basic rather than the web app's JWT/refresh-cookie
-- flow. Only a hash is stored, never the plaintext — it is shown to the user
-- exactly once, at creation time.
CREATE TABLE app_passwords (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    label TEXT NOT NULL,
    hash TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_used_at TIMESTAMP
);

CREATE INDEX idx_app_passwords_user_id ON app_passwords(user_id);

-- +goose Down
DROP TABLE app_passwords;
