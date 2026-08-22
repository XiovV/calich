-- +goose Up
-- Initial schema (ADR-0048). This file is a squash of the 44 migrations that
-- preceded it, produced by applying that chain to an empty database and
-- dumping the result — it is not a hand-written schema, and it is exactly
-- what the old chain produced.
--
-- Pre-1.0, migrations are squashed periodically rather than accumulated: the
-- intermediate states were never deployed anywhere and no database exists
-- that needs to travel through them. From the first release onwards this
-- file is frozen and migrations become strictly append-only. See ADR-0048.
--
-- The ADR/issue citations below are carried forward from the migrations that
-- introduced each table or column — they are the only pointer from a column
-- to the decision that shaped it.

-- Users (ADR-0010, ADR-0047).
--
-- email is the account's unique login identifier — web sign-in, CalDAV Basic
-- auth, and Share/Invite target resolution all resolve against it. It is
-- free of ':' and whitespace because it travels in a CalDAV Basic-auth
-- header, which net/http splits on the first colon. name is a non-unique
-- display label and may contain anything (ADR-0047).
--
-- email is COLLATE NOCASE (ADR-0058, #196): one address is one account
-- regardless of case, both at the UNIQUE constraint and at every WHERE
-- email = ? lookup, since SQLite resolves a bare column comparison's
-- collating sequence from the column itself. The app also folds case on
-- write (Register, UpdateEmail, Bootstrap, Workspace Invite issuance), which
-- this constraint alone can't provide: it's what makes a plain Go string
-- comparison between a stored email and a freshly typed one (e.g.
-- AcceptWorkspaceInviteExisting) agree without going through SQL.
--
-- synced_device_reminders_enabled resolves the CalDAV double-fire question
-- (ADR-0027): when on, the reminder scheduler skips the Notification channel
-- because the user's synced devices already raise their own pop-ups from the
-- synced VALARM. The Email channel is never gated by it — no device competes
-- for it. Off by default, so web-only users are unaffected.
--
-- is_disabled is a reversible account state, not a data operation (ADR-0037):
-- everything the Disabled User owns stays untouched, only authentication
-- stops.
--
-- week_start / default_view / time_format / working_hours_* are Preferences
-- (ADR-0039): per-User display settings the app previously decided by
-- accident. The Monday/24h defaults are deliberate, not a preservation of
-- any earlier implicit behaviour.
CREATE TABLE users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    password_hash TEXT NOT NULL,
    must_change_password INTEGER NOT NULL DEFAULT 0,
    email TEXT NOT NULL COLLATE NOCASE UNIQUE,
    synced_device_reminders_enabled INTEGER NOT NULL DEFAULT 0,
    is_disabled INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    week_start INTEGER NOT NULL DEFAULT 1,
    default_view TEXT NOT NULL DEFAULT 'week',
    time_format TEXT NOT NULL DEFAULT '24h',
    working_hours_start INTEGER NULL,
    working_hours_end INTEGER NULL,
    -- token_version (#242, ADR-0071) is embedded in every access token's "tv"
    -- claim at mint time and compared against the account's current value on
    -- every AuthService.Authenticate call. ChangePassword increments it, so a
    -- token minted before the change stops authenticating immediately rather
    -- than riding out its full accessTokenTTL. An integer counter rather than
    -- a password_changed_at timestamp because a JWT's NumericDate claims
    -- round-trip through JSON at whole-second precision — a password change
    -- landing in the same second as the token it's meant to outdate would
    -- make "issued before changed" ambiguous; a counter has no such tie.
    token_version INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE sessions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    refresh_token_hash TEXT NOT NULL UNIQUE,
    refresh_token_expires_at TIMESTAMP NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_sessions_refresh_token_hash ON sessions(refresh_token_hash);

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

-- Workspace (ADR-0044) is the account-management and billing boundary above
-- User: a named container with an Owner and an opaque subscription_status,
-- replacing the instance-wide Admin that ADR-0037 once defined.
--
-- owner_user_id carries no ON DELETE behaviour deliberately — Ownership
-- always transfers before a User can be deleted (ADR-0044's sole-Owner
-- guard), so a Workspace should never be left pointing at a deleted User.
--
-- default_share_privacy (#156) is configured by an Owner/Admin and read as
-- the default privacy a newly shared Calendar gets within the Workspace.
CREATE TABLE workspaces (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    owner_user_id INTEGER NOT NULL REFERENCES users(id),
    subscription_status TEXT NOT NULL DEFAULT 'none',
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    default_share_privacy TEXT NOT NULL DEFAULT 'private'
);

CREATE INDEX idx_workspaces_owner_user_id ON workspaces(owner_user_id);

-- workspace_members is a WorkspaceMember (ADR-0044): the row carrying a
-- User's Workspace Role inside one Workspace. The Owner also holds a row
-- here with role 'owner', so "who is in this Workspace" is always one query.
-- The user_id index drives "which Workspaces does this User belong to" — the
-- workspace switcher's data source.
CREATE TABLE workspace_members (
    workspace_id INTEGER NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role TEXT NOT NULL CHECK (role IN ('owner', 'admin', 'member')),
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (workspace_id, user_id)
);

CREATE INDEX idx_workspace_members_user_id ON workspace_members(user_id);

-- A Workspace Invite (ADR-0044) is hashed, single-use and expiring, but
-- targets an email plus a Workspace rather than living on a User row, since
-- the invited address may not have a User yet. UNIQUE(workspace_id, email)
-- means a workspace has at most one outstanding invite per email — issuing a
-- second one for the same pair is a reissue, not a new row.
CREATE TABLE workspace_invites (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    workspace_id INTEGER NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    email TEXT NOT NULL,
    invite_token_hash TEXT NOT NULL,
    invite_expires_at TIMESTAMP NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (workspace_id, email)
);

CREATE UNIQUE INDEX idx_workspace_invites_token_hash ON workspace_invites(invite_token_hash);

-- Calendars (ADR-0034, ADR-0045). user_id is the Owner — ownership stays
-- implicit here rather than as a row in calendar_shares, so a Calendar's
-- Access resolves as Owner first, then a Share's role, then None.
-- workspace_id is required, set at creation from whichever Workspace is
-- active, and never changes (#155, ADR-0045).
--
-- source_url makes a Calendar a Subscribed Calendar (#83, ADR-0032): a
-- non-null value binds it to an external .ics feed. NULL means an ordinary
-- Calendar, and every column below is NULL/0 for one.
--
-- etag / last_modified / content_hash / last_synced_at are Refresh's
-- conditional-GET and no-op-detection state (ADR-0033). etag and
-- last_modified mirror the feed's own response validators when it sends
-- them; content_hash is the fallback for a publisher that sends neither, so
-- a feed with no validators still costs a parse only when its body actually
-- changed.
--
-- next_refresh_at / refresh_interval_seconds / failure_count / error_class /
-- error_message are the background poller's scheduling and failure state
-- (#86, ADR-0033). refresh_interval_seconds is the publisher's own stated
-- cadence (RFC 7986 REFRESH-INTERVAL or X-PUBLISHED-TTL), honoured only when
-- longer than the poller's default. A broken feed is never disabled or
-- deleted: failure_count drives exponential backoff and resets to 0 on the
-- next success, and error_class is "needs_attention" (a human must fix
-- something) or "retrying" (expected to clear on its own).
--
-- keep_alarms (#87, ADR-0032) is off by default: a Refresh then drops the
-- feed's VALARMs exactly as Subscribe's initial import does; when true they
-- become Reminders on both Channels, matching ICS import.
--
-- feed_name / feed_color are shadow columns (#88, ADR-0032): the last value
-- the feed itself supplied (X-WR-CALNAME / X-APPLE-CALENDAR-COLOR), as
-- opposed to name/color, which are what's displayed. A Refresh updates the
-- displayed value only while it still equals its shadow — that comparison
-- alone is the "overridden by the User" flag, with no separate column.
--
-- color is an arbitrary sRGB hex value, not a member of a fixed enum
-- (ADR-0029); the eight Swatches the web app offers are a convenience for
-- the person picking, never a constraint on the stored value.
CREATE TABLE calendars (
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

CREATE INDEX idx_calendars_user_id ON calendars(user_id);
CREATE INDEX idx_calendars_workspace_id ON calendars(workspace_id);

-- calendar_shares is the Share (ADR-0034): the grant binding one Calendar to
-- one User with one Role. The Owner never has a row here. The user_id index
-- drives "which Calendars are shared with me" (ListSharedWithUser), the
-- other half of a Calendar list alongside calendars.user_id.
CREATE TABLE calendar_shares (
    calendar_id TEXT NOT NULL REFERENCES calendars(id) ON DELETE CASCADE,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role TEXT NOT NULL CHECK (role IN ('viewer', 'editor')),
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (calendar_id, user_id)
);

CREATE INDEX idx_calendar_shares_user_id ON calendar_shares(user_id);

-- groups is a Group (ADR-0045): a named set of a Workspace's Members,
-- created and membership-managed only by that Workspace's Owner or Admin. A
-- Group belongs to exactly one Workspace and never moves between them.
CREATE TABLE groups (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    workspace_id INTEGER NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_groups_workspace_id ON groups(workspace_id);

-- group_members (ADR-0045) is resolved dynamically — there is no
-- denormalized copy anywhere else — so a row here is the single source of
-- truth for "is this User in this Group right now".
CREATE TABLE group_members (
    group_id INTEGER NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (group_id, user_id)
);

CREATE INDEX idx_group_members_user_id ON group_members(user_id);

-- calendar_group_shares is the Group-targeted Share (ADR-0045), alongside
-- calendar_shares' per-User Share. Access resolution takes the best of
-- ownership, a direct Share, and a Share on a Group the User currently
-- belongs to — resolved dynamically off group_members, never expanded into a
-- snapshot here.
CREATE TABLE calendar_group_shares (
    calendar_id TEXT NOT NULL REFERENCES calendars(id) ON DELETE CASCADE,
    group_id INTEGER NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    role TEXT NOT NULL CHECK (role IN ('viewer', 'editor')),
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (calendar_id, group_id)
);

CREATE INDEX idx_calendar_group_shares_group_id ON calendar_group_shares(group_id);

-- calendar_user_colors is a per-User colour override (ADR-0038, amending
-- ADR-0029): calendars.color stays the Owner's and the default every new
-- Share inherits, but any User with Access may shadow it here with a colour
-- that applies to them alone. Keyed directly on (calendar_id, user_id) —
-- unlike event_reminders there is no wholesale-replace to survive, so
-- the override needs no indirection through another table.
CREATE TABLE calendar_user_colors (
    calendar_id TEXT NOT NULL REFERENCES calendars(id) ON DELETE CASCADE,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    color TEXT NOT NULL,
    PRIMARY KEY (calendar_id, user_id)
);

-- Events. An Event has no owner of its own — it inherits Access from its
-- Calendar (ADR-0034), which is why there is no user_id here.
--
-- rrule holds the iCalendar RRULE (RFC 5545) as opaque text, empty for a
-- non-recurring Event. The backend stays a thin CRUD store and does not
-- expand recurrence; the web frontend expands Occurrences over the visible
-- window (ADR-0016).
--
-- parent_id + recurrence_id turn a row into an Override: a complete
-- standalone Event replacing one Occurrence of its parent's series, keyed by
-- the Occurrence it replaces (its RECURRENCE-ID). Both are NULL on a Master,
-- recurring or not.
--
-- all_day (ADR-0017) reuses start/end, storing the half-open date range
-- (start = the date, end = the exclusive next day).
--
-- tzid anchors an Event's wall-clock to an IANA zone, an absolute instant
-- ("Etc/UTC"), or a Floating Event (NULL) — see ADR-0019.
--
-- external_uid is the foreign iCalendar UID a Refresh reconciles against
-- (ADR-0033), stored alongside the Event rather than as its id so the row id
-- stays a minted UUID.
--
-- url is the Event's optional link (iCalendar URL, RFC 5545), stored exactly
-- as written and never validated or normalized here — safety is enforced
-- once at render, not at this boundary (ADR-0063).
--
-- created_by is attribution only — who created the Event — and is
-- deliberately never consulted for authorization, so it can never be
-- mistaken for an access concept. SET NULL rather than CASCADE: a departing
-- household member's Events must not vanish from a Calendar they did not own.
--
-- color is an optional Event color in the same arbitrary-hex space as
-- Calendar color, winning outright over the Calendar's color when set
-- (ADR-0043). NULL means "inherit the Calendar's color".
--
-- sequence is this row's own iTIP SEQUENCE (ADR-0059, #201): distinct from
-- change_seq (CalDAV's per-object change marker, bumped on every write) and
-- never touched by one, it only increments when start, end, rrule or all_day
-- changes — RFC 5546's material-change set, the one that resets a recipient's
-- PARTSTAT — so a title fix or a colour change re-sends the Invitation
-- without wiping anyone's Response. Scoped to this row alone, not the whole
-- series: an Override tracks its own SEQUENCE independently of its Master's,
-- since each is its own VEVENT on the wire.
CREATE TABLE events (
    id TEXT PRIMARY KEY,
    calendar_id TEXT NOT NULL REFERENCES calendars(id) ON DELETE CASCADE,
    title TEXT NOT NULL,
    "start" TIMESTAMP NOT NULL,
    "end" TIMESTAMP NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    rrule TEXT NOT NULL DEFAULT '',
    parent_id TEXT REFERENCES events(id) ON DELETE CASCADE,
    recurrence_id TIMESTAMP,
    all_day INTEGER NOT NULL DEFAULT 0,
    tzid TEXT,
    description TEXT,
    location TEXT,
    url TEXT,
    change_seq INTEGER NOT NULL DEFAULT 0,
    external_uid TEXT,
    created_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
    color TEXT,
    sequence INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX idx_events_calendar_id ON events(calendar_id);
CREATE INDEX idx_events_parent_id ON events(parent_id);
CREATE INDEX idx_events_calendar_change_seq ON events(calendar_id, change_seq);

-- Unique per Calendar, and only enforced on Masters (parent_id IS NULL): an
-- Override legitimately shares its Master's iCalendar UID.
CREATE UNIQUE INDEX idx_events_calendar_external_uid ON events(calendar_id, external_uid)
  WHERE external_uid IS NOT NULL AND parent_id IS NULL;

-- Supports ListByCalendarIDs' windowed query (#80): filters on calendar_id
-- plus "start", and always orders by "start" — idx_events_calendar_id alone
-- can't serve either.
CREATE INDEX idx_events_calendar_start ON events(calendar_id, "start");

-- A cancelled single Occurrence (EXDATE) — the rule still generates that
-- slot, but it is suppressed from expansion (ADR-0016).
CREATE TABLE event_exceptions (
    parent_id TEXT NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    occurrence_start TIMESTAMP NOT NULL,
    PRIMARY KEY (parent_id, occurrence_start)
);

-- A Reminder is a trigger offset (minutes before Occurrence start) plus a
-- delivery channel, projecting an iCalendar VALARM — belonging to one User
-- on one Event, never to the Event itself, and never fanned out to anyone
-- else with Access (ADR-0064). Lives in a child table rather than a JSON
-- blob so the scheduler (ADR-0021) can query it. Many per (User, Event),
-- unconstrained: no cap, no dedupe, so no unique constraint on (user_id,
-- event_id, offset_minutes, channel). Wholesale-replaced per (User, Event)
-- on a Reminder write (ADR-0020) — an Editor's own Event edit never touches
-- another User's rows.
CREATE TABLE event_reminders (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    event_id TEXT NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    offset_minutes INTEGER NOT NULL,
    channel TEXT NOT NULL
);

CREATE INDEX idx_event_reminders_event_id ON event_reminders(event_id, user_id);

-- calendar_default_reminders is a User's own Default reminders on a
-- Calendar (ADR-0064): two independent lists per (User, Calendar), timed and
-- all-day, since an all-day Reminder's offset counts back from 09:00 on the
-- date rather than midnight (ADR-0020) — a single "30 minutes before" would
-- otherwise resolve to after the bins have gone. Applies to every Event on
-- the Calendar where that User has saved no list of their own; resolved
-- when Reminders are read, never copied onto an Event (see
-- event_reminders_explicit below for the "I have none, on purpose" marker).
-- Wholesale-replaced per (calendar_id, user_id, all_day) on a write, same
-- shape as event_reminders.
CREATE TABLE calendar_default_reminders (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    calendar_id TEXT NOT NULL REFERENCES calendars(id) ON DELETE CASCADE,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    all_day INTEGER NOT NULL DEFAULT 0,
    offset_minutes INTEGER NOT NULL,
    channel TEXT NOT NULL
);

CREATE INDEX idx_calendar_default_reminders_calendar_user ON calendar_default_reminders(calendar_id, user_id, all_day);

-- event_reminders_explicit is ADR-0064's one bit of resolution state: a
-- (User, Event) row here means that User's Reminder list on this Event is
-- explicit, even if it's currently empty, so their Calendar default no
-- longer applies to it. Written whenever a User saves their Reminder list
-- via its own write path (event_reminders_explicit.Mark) — including an
-- empty save, which is what "no Reminders on this one Event" means in
-- practice, since deleting the last row is the only UI for it. A CalDAV PUT
-- counts as one of those saves: its VALARM set is the PUTting principal's own
-- Reminder list, empty set included. Never written by an Event field write
-- that carries no Reminder list at all (Create/Update/ICS import).
CREATE TABLE event_reminders_explicit (
    event_id TEXT NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    PRIMARY KEY (event_id, user_id)
);

-- The firing engine's exactly-once ledger (ADR-0021): a Reminder+Occurrence
-- pair is recorded here the first time it fires, and never again — the
-- UNIQUE constraint is what makes a repeated tick or a process restart a
-- no-op for something already fired. user_id is not part of the key: a
-- Reminder id already implies its own User (ADR-0064), so no two Users can
-- ever share one — it is kept as an attribution column only, for dispatch.
CREATE TABLE fired_reminders (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    reminder_id INTEGER NOT NULL REFERENCES event_reminders(id) ON DELETE CASCADE,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    occurrence_start TIMESTAMP NOT NULL,
    fired_at TIMESTAMP NOT NULL,
    UNIQUE (reminder_id, occurrence_start)
);

-- fired_default_reminders is fired_reminders' counterpart for a Reminder
-- that fires by Calendar-default resolution rather than from the User's own
-- event_reminders row (ADR-0064) — it needs its own ledger because a
-- default_reminder_id names a (Calendar, User, timed/all-day) list, not one
-- Event, so the same default reminder fires independently across every
-- Event it resolves onto: the exactly-once key is (default_reminder_id,
-- event_id, occurrence_start), not just (default_reminder_id,
-- occurrence_start).
CREATE TABLE fired_default_reminders (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    default_reminder_id INTEGER NOT NULL REFERENCES calendar_default_reminders(id) ON DELETE CASCADE,
    event_id TEXT NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    occurrence_start TIMESTAMP NOT NULL,
    fired_at TIMESTAMP NOT NULL,
    UNIQUE (default_reminder_id, event_id, occurrence_start)
);

-- A Notification is the persistent in-app record of something that
-- concerns one User (ADR-0021, ADR-0061, CONTEXT.md) — widened from "what a
-- Notification-Channel Reminder produces when it fires" to also cover being
-- made an Attendee of an Event, distinguished by kind. Title is denormalized
-- (copied at write time) so a Notification keeps reading correctly even if
-- the Event's title later changes. occurrence_start is meaningful only for
-- kind = 'reminder' — an 'invite' concerns the Event series rather than one
-- Occurrence, so it's NULL there.
CREATE TABLE notifications (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    event_id TEXT NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    kind TEXT NOT NULL DEFAULT 'reminder' CHECK (kind IN ('reminder', 'invite')),
    occurrence_start TIMESTAMP,
    title TEXT NOT NULL,
    fired_at TIMESTAMP NOT NULL,
    seen BOOLEAN NOT NULL DEFAULT 0
);

CREATE INDEX idx_notifications_user_id ON notifications(user_id, fired_at DESC);

-- attendees is an Attendee (ADR-0046): a User or a bare email address
-- (ADR-0058, #200) invited to one specific Event, independent of Calendar
-- Access — the invite itself is the grant, scoped to that Event alone.
-- response is the iCalendar PARTSTAT the Attendee has set on their own
-- invite; an email-shaped Attendee has no account to set it through and
-- stays needs-action forever. The user_id index drives "which Events is
-- this User an Attendee of" — the visible-events union's reverse-lookup
-- direction.
--
-- user_id and email are both nullable with exactly one set (ADR-0058): one
-- table, one Response state machine, one list, one ATTENDEE line, rather
-- than a second entity that differs from this one in nothing a caller cares
-- about. email is COLLATE NOCASE for the same reason users.email is
-- (case-insensitive matching against a typed address). The old
-- PRIMARY KEY (event_id, user_id) constrained nothing once user_id allows
-- NULLs — SQLite treats every NULL as distinct — so it's replaced by two
-- UNIQUE constraints, one per shape, which is what actually stops the same
-- person being invited twice under either.
CREATE TABLE attendees (
    event_id TEXT NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    user_id INTEGER REFERENCES users(id) ON DELETE CASCADE,
    email TEXT COLLATE NOCASE,
    response TEXT NOT NULL DEFAULT 'needs-action' CHECK (response IN ('needs-action', 'accepted', 'declined', 'tentative')),
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CHECK ((user_id IS NULL) <> (email IS NULL)),
    UNIQUE (event_id, user_id),
    UNIQUE (event_id, email)
);

CREATE INDEX idx_attendees_user_id ON attendees(user_id);

-- Attachments (#132, ADR-0040): a file uploaded to this instance and held
-- against a Master, shown on every Occurrence of its series. An Override
-- cannot carry one of its own — enforced in the service layer (a Master's id
-- is required), not by a constraint here, since SQLite has no partial
-- foreign key.
--
-- id is also the filename on disk (DATA_DIR/attachments/<id[:2]>/<id>) and
-- the RFC 8607 MANAGED-ID; minted server-side only, never accepted from a
-- client. filename is the user's own name for the file, used only in
-- Content-Disposition — nothing user-supplied ever reaches the filesystem.
--
-- uploaded_by is attribution only, mirroring events.created_by: never
-- consulted for authorization, which resolves through the Event's Calendar
-- instead (ADR-0034).
CREATE TABLE event_attachments (
    id           TEXT PRIMARY KEY,
    event_id     TEXT NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    filename     TEXT NOT NULL,
    content_type TEXT NOT NULL,
    size_bytes   INTEGER NOT NULL,
    uploaded_by  INTEGER REFERENCES users(id) ON DELETE SET NULL,
    created_at   TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_event_attachments_event_id ON event_attachments(event_id);

-- CalDAV sync-token bookkeeping. change_sequence is a single-row counter
-- (the CHECK pins it to id = 1) that every Event mutation bumps, stamped
-- onto events.change_seq; deleted_objects records what a sync must report as
-- gone once the Event row itself is no longer there.
CREATE TABLE change_sequence (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    value INTEGER NOT NULL
);

-- The singleton counter row. Not optional seed data: the sync path reads and
-- bumps this row rather than creating it, so a database without it cannot
-- serve CalDAV sync at all.
INSERT INTO change_sequence (id, value) VALUES (1, 0);

CREATE TABLE deleted_objects (
    calendar_id TEXT NOT NULL REFERENCES calendars(id) ON DELETE CASCADE,
    uid TEXT NOT NULL,
    change_seq INTEGER NOT NULL
);

CREATE INDEX idx_deleted_objects_calendar_change_seq ON deleted_objects(calendar_id, change_seq);

-- outbox is a queued Invitation or Cancellation email (ADR-0059, ADR-0060,
-- #201): one row per Attendee row it accompanies, written in the same
-- transaction so a failed send never loses the Attendee and a rolled-back
-- create queues nothing. A background ticker drains rows in id order (oldest
-- first), which is what makes delivery per-recipient ordered without any
-- locking — a later message can never be sent before an earlier one to the
-- same recipient resolves, so a CANCEL can never overtake the REQUEST it
-- withdraws. recipient_user_id and recipient_email are nullable with exactly
-- one set (#200, ADR-0058): a User-backed Attendee queues the former, an
-- email-shaped one — no account to notify in-app, but still an Invitation
-- to send — the latter. recipient_email is COLLATE NOCASE for the same
-- reason attendees.email is, even though nothing queries this column by
-- value today (the Worker's per-recipient blocking key folds case in Go
-- instead) — case-insensitivity is this app's baseline for every email
-- column, not an opt-in per query.
--
-- event_id carries no REFERENCES: a CANCEL is queued for exactly the moment
-- the Event row (or the Attendee row it withdraws) stops existing, so an
-- ON DELETE CASCADE here would erase the very message being queued to
-- explain that deletion, in the same transaction that deletes it. snapshot
-- is a CANCEL-only, JSON-encoded repository.OutboxCancelSnapshot: everything
-- InvitationSender needs to render a METHOD:CANCEL, captured while the row
-- it withdraws still exists — NULL on a REQUEST, which always renders from
-- live state instead (InvitationSender.Send's own contract).
--
-- actor_user_id names who queued a brand-new Invitation (#204, ADR-0058) —
-- set only by inviteUser/inviteEmail/expandGroupMembers, the three paths
-- that invite someone not previously an Attendee. It is left NULL on every
-- re-send (Update's material-change re-issue, AddAttendee/AddGroupAttendee/
-- RemoveAttendee's re-send to everyone else) and every CANCEL, so a per-User
-- hourly count of non-NULL rows counts exactly "Invitations this User
-- caused to be sent to someone new" — lifecycle mail for an Event already
-- invited never counts against it. ON DELETE SET NULL rather than CASCADE:
-- the actor's own account going away must not erase the queued mail itself.
CREATE TABLE outbox (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    event_id          TEXT NOT NULL,
    recipient_user_id INTEGER REFERENCES users(id) ON DELETE CASCADE,
    recipient_email   TEXT COLLATE NOCASE,
    actor_user_id     INTEGER REFERENCES users(id) ON DELETE SET NULL,
    method            TEXT NOT NULL DEFAULT 'REQUEST' CHECK (method IN ('REQUEST', 'CANCEL')),
    snapshot          TEXT,
    status            TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'sent', 'failed')),
    attempts          INTEGER NOT NULL DEFAULT 0,
    next_attempt_at   TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_error        TEXT,
    created_at        TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    sent_at           TIMESTAMP,
    CHECK ((recipient_user_id IS NULL) <> (recipient_email IS NULL))
);

CREATE INDEX idx_outbox_status_id ON outbox(status, id);
CREATE INDEX idx_outbox_actor_created ON outbox(actor_user_id, created_at);

-- auth_rate_limit_attempts is AuthRateLimiter's rolling-window ledger (#240,
-- ADR-0070): one row per throttled attempt against Login, CalDAV Basic auth,
-- or Register. scope is 'auth' (Login and CalDAV Basic auth share one
-- ceiling — the same "guess a password against one account" problem behind
-- two transports) or 'register'. key_type is 'email' or 'ip': Register has
-- no existing credential to key on, so it's counted by ip alone, while auth
-- is counted by both independently (either bucket at its ceiling refuses
-- the request) since neither key alone resists both a distributed guess
-- (many IPs, one email) and a guess that rotates targets (one IP, many
-- emails). key_value is COLLATE NOCASE for its 'email' rows, the same fold
-- every other email column in this schema carries; harmless for 'ip' rows,
-- which never vary by case. There is no user_id/actor column, unlike
-- outbox's actor-keyed ceiling (ADR-0058) — by definition a failed Login has
-- no authenticated actor yet to key on.
CREATE TABLE auth_rate_limit_attempts (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    scope      TEXT NOT NULL CHECK (scope IN ('auth', 'register')),
    key_type   TEXT NOT NULL CHECK (key_type IN ('email', 'ip')),
    key_value  TEXT NOT NULL COLLATE NOCASE,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_auth_rate_limit_lookup ON auth_rate_limit_attempts(scope, key_type, key_value, created_at);

-- +goose Down
DROP TABLE auth_rate_limit_attempts;
DROP TABLE outbox;
DROP TABLE deleted_objects;
DROP TABLE change_sequence;
DROP TABLE event_attachments;
DROP TABLE attendees;
DROP TABLE notifications;
DROP TABLE fired_reminders;
DROP TABLE event_reminders;
DROP TABLE event_exceptions;
DROP TABLE events;
DROP TABLE calendar_user_colors;
DROP TABLE calendar_group_shares;
DROP TABLE group_members;
DROP TABLE groups;
DROP TABLE calendar_shares;
DROP TABLE calendars;
DROP TABLE workspace_invites;
DROP TABLE workspace_members;
DROP TABLE workspaces;
DROP TABLE app_passwords;
DROP TABLE sessions;
DROP TABLE users;
