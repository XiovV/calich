-- +goose Up
-- booking_links and booking_link_conflict_calendars are a Booking Link
-- (#322, ADR-0084, ADR-0087, CONTEXT.md's Booking Link entry): a named,
-- slug-addressed offer to be booked, belonging to one User within one
-- Workspace — scoped and cascaded on both exactly like task_lists
-- (00010_task_lists.sql), since a link writes into a Calendar of that
-- Workspace and dies with the membership that gave it one.
--
-- slug is unique per User, not instance-wide (ADR-0084: "two people may
-- both publish intro-call"), hence UNIQUE(user_id, slug) rather than a
-- bare UNIQUE(slug) the way users.handle is. COLLATE NOCASE folds case the
-- same way handle and email do.
--
-- availability_schedule_id and book_into_calendar_id carry no ON DELETE
-- clause (SQLite's default, NO ACTION, behaves like RESTRICT under the
-- foreign_keys pragma this app always runs with) — deleting a Schedule or
-- Calendar still referenced by a Booking Link is refused at the database
-- level, which is what backs ADR-0085's "deleting a Schedule still
-- referenced is refused" now that a referencing entity finally exists.
--
-- minimum_notice_minutes and booking_horizon_days default to ADR-0087's own
-- stated defaults (4 hours, 60 days) so a link created without opening
-- More options is already complete. tasks_in_conflict_set is the Conflict
-- set's "single row standing for Tasks" (CONTEXT.md) — a plain boolean
-- here rather than a synthetic row in the join table below, since Tasks
-- has no calendar_id to put there.
CREATE TABLE booking_links (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    workspace_id INTEGER NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    title TEXT NOT NULL,
    slug TEXT NOT NULL COLLATE NOCASE,
    duration_minutes INTEGER NOT NULL,
    visibility TEXT NOT NULL DEFAULT 'public' CHECK (visibility IN ('public', 'private', 'paused')),
    availability_schedule_id INTEGER NOT NULL REFERENCES availability_schedules(id),
    book_into_calendar_id TEXT NOT NULL REFERENCES calendars(id),
    location TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    minimum_notice_minutes INTEGER NOT NULL DEFAULT 240,
    booking_horizon_days INTEGER NOT NULL DEFAULT 60,
    tasks_in_conflict_set INTEGER NOT NULL DEFAULT 1,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (user_id, slug)
);

CREATE INDEX idx_booking_links_user_id ON booking_links(user_id);
CREATE INDEX idx_booking_links_workspace_id ON booking_links(workspace_id);

-- The Calendar half of the Conflict set (ADR-0087) — seeded from every
-- Calendar the User owns in the Workspace, plus the Book-into Calendar
-- itself, unremovable. Shaped after calendar_set_members
-- (00009_calendar_sets.sql).
CREATE TABLE booking_link_conflict_calendars (
    link_id INTEGER NOT NULL REFERENCES booking_links(id) ON DELETE CASCADE,
    calendar_id TEXT NOT NULL REFERENCES calendars(id) ON DELETE CASCADE,
    PRIMARY KEY (link_id, calendar_id)
);

CREATE INDEX idx_booking_link_conflict_calendars_calendar_id ON booking_link_conflict_calendars(calendar_id);

-- +goose Down
DROP TABLE booking_link_conflict_calendars;
DROP TABLE booking_links;
