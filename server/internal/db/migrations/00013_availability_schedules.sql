-- +goose Up
-- availability_schedules and availability_schedule_ranges are an
-- Availability Schedule (#320, ADR-0085, CONTEXT.md's Availability Schedule
-- entry): a named weekly pattern of time ranges belonging to one User,
-- carrying its own IANA timezone, from which a Booking Link's slots will be
-- derived (#323). Deliberately not scoped to a Workspace, unlike
-- calendar_sets and task_lists (00009, 00010) — a Schedule belongs to the
-- User alone, and is reusable across every Workspace a Booking Link of
-- theirs might write into.
--
-- One is auto-created per User, named "Default" and seeded once from that
-- User's working_hours_start/working_hours_end (Mon-Fri 09:00-17:00 when
-- unset); editing Working hours afterwards never reaches a Schedule, ever
-- (ADR-0085's central rule) — there is no foreign key back to
-- working_hours_start/end, only a one-time copy at seed time.
--
-- Ranges are shaped after calendar_sets' own append-only era (ADR-0048):
-- weekday (0-6, Sunday-Saturday, matching users.week_start's own convention)
-- plus a start/end minute-of-day pair, several per weekday permitted so a
-- split day is representable.
CREATE TABLE availability_schedules (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    tzid TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_availability_schedules_user_id ON availability_schedules(user_id);

CREATE TABLE availability_schedule_ranges (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    schedule_id INTEGER NOT NULL REFERENCES availability_schedules(id) ON DELETE CASCADE,
    weekday INTEGER NOT NULL,
    start_minute INTEGER NOT NULL,
    end_minute INTEGER NOT NULL
);

CREATE INDEX idx_availability_schedule_ranges_schedule_id ON availability_schedule_ranges(schedule_id);

-- +goose Down
DROP TABLE availability_schedule_ranges;
DROP TABLE availability_schedules;
