-- +goose Up
-- Every Calendar belongs to exactly one Workspace (#155, ADR-0045):
-- workspace_id is required, set at creation time from whichever Workspace
-- is active, and never changes. SQLite can't add a NOT NULL column with no
-- default to a populated table, so the table is rebuilt (same pattern as
-- 00027's fired_reminders rebuild).
CREATE TABLE calendars_new (
    id TEXT PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    workspace_id INTEGER NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    color TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    source_url TEXT,
    last_synced_at TIMESTAMP,
    etag TEXT,
    last_modified TEXT,
    content_hash TEXT,
    next_refresh_at TIMESTAMP,
    refresh_interval_seconds INTEGER,
    failure_count INTEGER NOT NULL DEFAULT 0,
    error_class TEXT,
    error_message TEXT,
    keep_alarms BOOLEAN NOT NULL DEFAULT 0,
    feed_name TEXT,
    feed_color TEXT
);

-- Backfill: every existing Calendar moves into a Workspace owned by its
-- current owner (ADR-0045's acceptance criteria) — the Workspace ADR-0044's
-- bootstrap/self-registration already created for that owner. Every User
-- with a pre-existing Calendar has exactly one owned Workspace at this
-- point in the migration history, so the join is unambiguous.
INSERT INTO calendars_new
SELECT c.id, c.user_id, w.id, c.name, c.color, c.created_at, c.source_url,
       c.last_synced_at, c.etag, c.last_modified, c.content_hash,
       c.next_refresh_at, c.refresh_interval_seconds, c.failure_count,
       c.error_class, c.error_message, c.keep_alarms, c.feed_name, c.feed_color
FROM calendars c
JOIN workspaces w ON w.owner_user_id = c.user_id;

DROP TABLE calendars;
ALTER TABLE calendars_new RENAME TO calendars;

CREATE INDEX idx_calendars_user_id ON calendars(user_id);
CREATE INDEX idx_calendars_workspace_id ON calendars(workspace_id);

-- +goose Down
CREATE TABLE calendars_old (
    id TEXT PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    color TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    source_url TEXT,
    last_synced_at TIMESTAMP,
    etag TEXT,
    last_modified TEXT,
    content_hash TEXT,
    next_refresh_at TIMESTAMP,
    refresh_interval_seconds INTEGER,
    failure_count INTEGER NOT NULL DEFAULT 0,
    error_class TEXT,
    error_message TEXT,
    keep_alarms BOOLEAN NOT NULL DEFAULT 0,
    feed_name TEXT,
    feed_color TEXT
);
INSERT INTO calendars_old
SELECT id, user_id, name, color, created_at, source_url, last_synced_at, etag,
       last_modified, content_hash, next_refresh_at, refresh_interval_seconds,
       failure_count, error_class, error_message, keep_alarms, feed_name, feed_color
FROM calendars;
DROP TABLE calendars;
ALTER TABLE calendars_old RENAME TO calendars;
CREATE INDEX idx_calendars_user_id ON calendars(user_id);
