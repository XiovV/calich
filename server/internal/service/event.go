package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/XiovV/calendar/server/internal/icalendar"
	"github.com/XiovV/calendar/server/internal/recurrence"
	"github.com/XiovV/calendar/server/internal/repository"
)

var (
	ErrInvalidTitle     = errors.New("event title must not be empty")
	ErrInvalidTimeRange = errors.New("event end must be after start")
	// ErrInvalidRecurrenceRule is returned when a non-empty rrule fails the
	// backend's light sanity check. The backend treats rrule as opaque text and
	// trusts the frontend-authored rule (ADR-0016), so this only guards against
	// obviously malformed input, not full RFC 5545 validation.
	ErrInvalidRecurrenceRule = errors.New("recurrence rule is invalid")
	// ErrCalendarNotFound is returned instead of repository.ErrNotFound when
	// an event's calendar_id doesn't resolve for the caller, so handlers can
	// tell it apart from the event itself not being found.
	ErrCalendarNotFound = errors.New("calendar not found")
	// ErrParentNotFound is returned when an Override's parentId doesn't resolve
	// for the caller.
	ErrParentNotFound = errors.New("parent event not found")
	// ErrInvalidOverride is returned when an Override is created with an rrule
	// of its own, or is missing its recurrenceId — an Override is a complete
	// standalone instance, never itself a Master (ADR-0016).
	ErrInvalidOverride = errors.New("an override must not have its own recurrence rule, and requires a recurrence id")
	// ErrParentIsOverride is returned when parentId names an Override rather
	// than a Master — Overrides cannot themselves be overridden or excepted.
	ErrParentIsOverride = errors.New("parent event must be a master, not an override")
	// ErrParentNotRecurring is returned when adding an Exception to a parent
	// that has no recurrence rule — a non-recurring Master has nothing to
	// except.
	ErrParentNotRecurring = errors.New("parent event does not recur")
	// ErrInvalidReminderChannel is returned when a Reminder names a channel
	// other than "notification" or "email" — the only Channels ADR-0020
	// defines.
	ErrInvalidReminderChannel = errors.New("reminder channel must be \"notification\" or \"email\"")
	// ErrOccurrenceNotFound is returned when occurrenceStart doesn't name a
	// real Occurrence of the series: it isn't dtstart nor a start the rrule
	// generates, or it's excluded by an Exdate (#76).
	ErrOccurrenceNotFound = errors.New("occurrence not found")
	// ErrInvalidEventColor is returned when an Event's color fails
	// NormalizeColor — the same arbitrary-hex value space as Calendar color
	// (ADR-0029, ADR-0043).
	ErrInvalidEventColor = errors.New("invalid event color")
	// ErrCalendarReadOnly is returned by every mutating method (Create,
	// Update, Delete, AddException, ReparentFrom, ImportSeries, PutSeries)
	// when the caller's Access to the Calendar a write targets doesn't
	// clear CanWrite — a Viewer Share (ADR-0034), or a Calendar carrying a
	// SourceURL, whose Events are written only by Refresh's bypass,
	// ImportSubscribedSeries, for Owner and Editor alike (ADR-0032). The
	// guard lives here rather than at the REST/CalDAV edges so every entry
	// point is covered by construction.
	ErrCalendarReadOnly = errors.New("calendar is read-only")
	// ErrAttendeeTargetNotInWorkspace is returned by AddAttendee when
	// targetUserID, or by AddGroupAttendee when groupID, doesn't belong to
	// the Event's Calendar's own Workspace (#161, #162, ADR-0046) — an
	// Attendee invite, like a Share, can only ever reach someone (or some
	// Group) already inside that Workspace.
	ErrAttendeeTargetNotInWorkspace = errors.New("attendee target does not belong to this workspace")
	// ErrInvalidResponse is returned by SetResponse when response isn't one
	// of the iCalendar PARTSTAT values ADR-0046 defines.
	ErrInvalidResponse = errors.New("response must be \"needs-action\", \"accepted\", \"declined\", or \"tentative\"")
	// ErrAttendeeIsOrganizer is returned by inviteEmail when a typed address
	// (ADR-0058, #200) matches the Event's own Organizer, whether typed by
	// the Organizer themselves or by an Editor. Rejected rather than
	// silently dropped: ADR-0055 made the Organizer structurally not an
	// Attendee, and admitting the row is one SetResponse away from an
	// Organizer who declined their own meeting.
	ErrAttendeeIsOrganizer = errors.New("cannot invite the event's organizer as an attendee")
	// ErrAttendeeEmailInvitesUnavailable is returned by inviteEmail when a
	// typed address resolves to no account on this instance and outbox is
	// nil (no SMTP configured, ADR-0059). Mirrors the picker-side posture
	// ADR-0058 already takes ("the affordance is absent entirely") applied
	// to the write path: an email-shaped row nobody can ever deliver to is
	// not a degraded invite, it's a row that means nothing, so it's refused
	// rather than written.
	ErrAttendeeEmailInvitesUnavailable = errors.New("email invitations are not available on this instance")
	// ErrInviteRateLimitExceeded is InviteRateLimitError's sentinel, for call
	// sites that only need to recognize the failure by identity rather than
	// name the configured ceiling (#204, ADR-0058).
	ErrInviteRateLimitExceeded = errors.New("invitation rate limit exceeded")
)

// InviteRateLimitError wraps ErrInviteRateLimitExceeded with the actor's own
// configured hourly ceiling (#204, ADR-0058), so the error names the limit
// that was hit rather than a generic refusal — an organizer must never
// believe an invitation went out when it did not, and "try again, some of
// this silently didn't happen" is the failure mode the outbox itself
// (ADR-0060) exists to rule out. Returned by inviteUser/inviteEmail/
// expandGroupMembers alone — the three paths that invite someone not
// already an Attendee — never by a re-send or a cancellation, both of which
// are lifecycle mail for an Event already invited to and stay uncapped.
type InviteRateLimitError struct {
	LimitPerHour int
}

func (e *InviteRateLimitError) Error() string {
	return fmt.Sprintf("invitation rate limit exceeded: at most %d invitations per hour", e.LimitPerHour)
}

func (e *InviteRateLimitError) Unwrap() error {
	return ErrInviteRateLimitExceeded
}

// chargeInviteRateLimit enforces actorUserID's own hourly ceiling on
// brand-new Invitations (#204, ADR-0058) before inviteUser/inviteEmail/
// expandGroupMembers queue one more — counting only what
// OutboxRepository.EnqueueWithActor/EnqueueEmailWithActor wrote them, never
// a re-send or a CANCEL (repository.OutboxMessage.ActorUserID's own
// contract), so lifecycle mail for an Event already invited to never trips
// it. Runs inside the same transaction as the write it's guarding, so
// hitting the ceiling partway through a multi-target invite (a Create
// carrying several targets, or a large Group expansion) fails the whole
// write via withTx's rollback rather than leaving some recipients invited
// and others silently not.
//
// This is a deliberate exception to ADR-0018's general "reads happen
// outside the transaction" convention, on the same grounds
// expandGroupMembers' own caller (AddGroupAttendee) already carries for
// reading Group membership inside its transaction: a count-then-charge read
// outside the write it gates is a TOCTOU gap — two concurrent invites could
// each read the same count and both proceed, both landing past the
// ceiling. Reading and writing the charge together, inside one transaction,
// is what makes the check exact rather than advisory.
func chargeInviteRateLimit(ctx context.Context, outbox *repository.OutboxRepository, actorUserID int64, limitPerHour int) error {
	count, err := outbox.CountByActorSince(ctx, actorUserID, time.Now().Add(-time.Hour))
	if err != nil {
		return fmt.Errorf("count invitations for rate limit: %w", err)
	}
	if count >= limitPerHour {
		return &InviteRateLimitError{LimitPerHour: limitPerHour}
	}
	return nil
}

// isValidReminderChannel reports whether channel is one of the Channels
// ADR-0020 defines. AUDIO and other iCalendar VALARM actions are out of
// scope.
func isValidReminderChannel(channel string) bool {
	return channel == "notification" || channel == "email"
}

func validateReminders(reminders []repository.Reminder) error {
	for _, reminder := range reminders {
		if !isValidReminderChannel(reminder.Channel) {
			return ErrInvalidReminderChannel
		}
	}
	return nil
}

// normalizeEventColor validates color against NormalizeColor and returns its
// canonical form — the same arbitrary-hex value space as Calendar color
// (ADR-0029, ADR-0043). A nil color (inherit the Calendar's color) passes
// through unchanged; there is nothing to validate.
func normalizeEventColor(color *string) (*string, error) {
	if color == nil {
		return nil, nil
	}
	normalized, ok := NormalizeColor(*color)
	if !ok {
		return nil, ErrInvalidEventColor
	}
	return &normalized, nil
}

// colorEqual reports whether a and b name the same Event color override,
// including both being nil ("inherit the Calendar's color", ADR-0043).
func colorEqual(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// materialFieldsChanged reports whether write's start, end, rrule or all_day
// differ from existing's — the ADR-0059 material-change set: per RFC 5546,
// changing any of these is what must reset a recipient's PARTSTAT back to
// needs-action, via a re-issued Invitation carrying an incremented SEQUENCE
// (#201). Time.Equal, not ==, since existing came back from a database
// round-trip and write from the caller.
func materialFieldsChanged(existing repository.Event, write EventWrite) bool {
	return !existing.Start.Equal(write.Start) || !existing.End.Equal(write.End) || existing.Rrule != write.Rrule || existing.AllDay != write.AllDay
}

type EventService struct {
	db            *sql.DB
	events        *repository.EventRepository
	exceptions    *repository.EventExceptionRepository
	reminders     *repository.EventReminderRepository
	sync          *repository.SyncRepository
	calendars     *CalendarService
	users         *repository.UserRepository
	attachments   *repository.AttachmentRepository
	attendees     *repository.AttendeeRepository
	workspaces    *repository.WorkspaceRepository
	groups        *repository.GroupRepository
	notifications *repository.NotificationRepository
	// outbox queues an Invitation alongside every Attendee row a User is
	// named on (ADR-0059, ADR-0060) — nil when this deployment has no SMTP
	// transport configured, in which case inviteUser and expandGroupMembers
	// simply queue nothing (ADR-0059's "no affordance" posture already
	// governs who can be invited at all; this is the same posture applied to
	// the write path).
	outbox *repository.OutboxRepository
	// inviteRateLimitPerHour is the per-User hourly ceiling on brand-new
	// Invitations chargeInviteRateLimit enforces (#204, ADR-0058) —
	// INVITE_RATE_LIMIT_PER_HOUR, or its default, resolved once at startup
	// (config.Config).
	inviteRateLimitPerHour int
}

func NewEventService(db *sql.DB, events *repository.EventRepository, exceptions *repository.EventExceptionRepository, reminders *repository.EventReminderRepository, sync *repository.SyncRepository, calendars *CalendarService, users *repository.UserRepository, attachments *repository.AttachmentRepository, attendees *repository.AttendeeRepository, workspaces *repository.WorkspaceRepository, groups *repository.GroupRepository, notifications *repository.NotificationRepository, outbox *repository.OutboxRepository, inviteRateLimitPerHour int) *EventService {
	return &EventService{db: db, events: events, exceptions: exceptions, reminders: reminders, sync: sync, calendars: calendars, users: users, attachments: attachments, attendees: attendees, workspaces: workspaces, groups: groups, notifications: notifications, outbox: outbox, inviteRateLimitPerHour: inviteRateLimitPerHour}
}

// calendarByID resolves calendarID via s.calendars.Get, translating
// repository.ErrNotFound to ErrCalendarNotFound so every write path reports
// the same sentinel a caller already handles. The writes allowed to target
// a Subscribed Calendar — ImportSubscribedSeries and ReconcileSubscribedSeries
// (#85, ADR-0033) — call this directly, skipping requireWritableCalendar's
// guard; every other mutating method calls that instead.
// asCalendarNotFound translates repository.ErrNotFound to
// ErrCalendarNotFound, the sentinel calendarByID and requireWritableCalendar
// both return instead, so a caller can't tell "no such calendar" apart from
// "no such event" by error identity alone.
func asCalendarNotFound(err error) error {
	if errors.Is(err, repository.ErrNotFound) {
		return ErrCalendarNotFound
	}
	return err
}

func (s *EventService) calendarByID(ctx context.Context, userID int64, calendarID string) (repository.Calendar, error) {
	calendar, err := s.calendars.Get(ctx, userID, calendarID)
	if err != nil {
		return repository.Calendar{}, asCalendarNotFound(err)
	}
	return calendar, nil
}

// reminderOwnerID resolves calendarID's Owner id for a Reminder read or
// write seam (ADR-0064 step one, #208), wrapping repository.ErrNotFound the
// same way calendarByID does. The caller has always already established its
// own Access to calendarID some other way (requireWritableCalendar,
// getOwnedEvent, ...), so this needs no userID of its own — mirroring
// CalendarService.OwnerID's own access-unchecked contract.
func (s *EventService) reminderOwnerID(ctx context.Context, calendarID string) (int64, error) {
	ownerID, err := s.calendars.OwnerID(ctx, calendarID)
	if err != nil {
		return 0, asCalendarNotFound(err)
	}
	return ownerID, nil
}

// requireWritableCalendar resolves the caller's Access to calendarID and
// refuses it unless that Access can write — false for a stranger (None), a
// Viewer Share (ADR-0034), and, per ADR-0032's clamp, for a Subscribed
// Calendar's Owner and Editor alike (Viewer) — in one call: the guard every
// mutating method except ImportSubscribedSeries applies to every Calendar
// its write touches. This is the Subscribed Calendar write guard expressed
// through the Access resolver rather than beside it: ResolveAccess is what
// now decides it's read-only, not a SourceURL check made here.
func (s *EventService) requireWritableCalendar(ctx context.Context, userID int64, calendarID string) error {
	access, _, err := s.calendars.Access(ctx, userID, calendarID)
	if err != nil {
		return asCalendarNotFound(err)
	}
	if !access.CanRead() {
		return ErrCalendarNotFound
	}
	if !access.CanWrite() {
		return ErrCalendarReadOnly
	}
	return nil
}

// getOwnedEvent resolves id and checks the caller's Access to its Calendar,
// in one call — the single seam every method that reads or writes one
// Event by id funnels through, now that an Event has no owner of its own
// and Access resolves through CalendarID instead (ADR-0034). Returns
// repository.ErrNotFound both when id doesn't exist and when it does but
// the caller has no Access to its Calendar — the same sentinel the old
// user_id-filtered query returned in both cases, so no caller's error
// handling changes.
func (s *EventService) getOwnedEvent(ctx context.Context, userID int64, id string) (repository.Event, error) {
	event, err := s.events.GetByID(ctx, id)
	if err != nil {
		return repository.Event{}, err
	}
	if _, err := s.calendarByID(ctx, userID, event.CalendarID); err != nil {
		if errors.Is(err, ErrCalendarNotFound) {
			return repository.Event{}, repository.ErrNotFound
		}
		return repository.Event{}, err
	}
	return event, nil
}

// getVisibleEvent resolves id and confirms userID may see it — either via
// Calendar Access (getOwnedEvent) or, failing that, by being one of its
// Attendees (ADR-0046, #161): an Attendee invite grants visibility to that
// one Event with no Calendar Access of its own. Every Event-field write path
// keeps calling getOwnedEvent/requireWritableCalendar directly, since an
// Attendee with no Calendar Access can never write one — GetReminders and
// SetReminders are the exception (#211), since a Reminder is personal state
// rather than an Event field (ADR-0064).
func (s *EventService) getVisibleEvent(ctx context.Context, userID int64, id string) (repository.Event, error) {
	event, err := s.getOwnedEvent(ctx, userID, id)
	if err == nil {
		return event, nil
	}
	if !errors.Is(err, repository.ErrNotFound) {
		return repository.Event{}, err
	}

	if _, attErr := s.attendees.Get(ctx, id, userID); attErr != nil {
		if errors.Is(attErr, repository.ErrNotFound) {
			return repository.Event{}, repository.ErrNotFound
		}
		return repository.Event{}, attErr
	}

	return s.events.GetByID(ctx, id)
}

// GetReminders returns userID's own Reminders on eventID (ADR-0064) — empty,
// not an error, if userID has never set any. Anyone who can see eventID may
// call this — Owner, Editor, Viewer, or a User-backed Attendee with no
// Calendar Access at all (ADR-0046, ADR-0058) — via getVisibleEvent, exactly
// the old fan-out's recipient set now describing who may set their own
// Reminder rather than who gets nagged (#211).
func (s *EventService) GetReminders(ctx context.Context, userID int64, eventID string) ([]repository.Reminder, error) {
	if _, err := s.getVisibleEvent(ctx, userID, eventID); err != nil {
		return nil, err
	}

	byEvent, err := s.reminders.ListByEventIDs(ctx, userID, []string{eventID})
	if err != nil {
		return nil, fmt.Errorf("get reminders: %w", err)
	}
	return byEvent[eventID], nil
}

// SetReminders replaces userID's own Reminders on eventID wholesale
// (ADR-0020, ADR-0064) — a Reminder is personal outright, so this never
// touches another User's rows on the same Event, its title, or its
// SEQUENCE (no Invitation is re-sent, ADR-0059). It does bump eventID's own
// change_seq, so a per-principal CalDAV object (#210) reaches the caller's
// other devices on their next sync — and only theirs, since a CTag bump is
// still shared by the whole Calendar (ADR-0064's accepted CTag waste).
// Not an Event write (#211): it applies to the same recipient set as
// GetReminders — visibility via getVisibleEvent is enough, with no
// requireWritableCalendar call layered on top. It therefore bypasses the
// Viewer restriction and a Source's read-only clamp alike, exactly as
// SetColorOverride does for a Calendar colour (ADR-0038).
func (s *EventService) SetReminders(ctx context.Context, userID int64, eventID string, reminders []repository.Reminder) ([]repository.Reminder, error) {
	if err := validateReminders(reminders); err != nil {
		return nil, err
	}

	if _, err := s.getVisibleEvent(ctx, userID, eventID); err != nil {
		return nil, err
	}

	if err := s.reminders.ReplaceByEventID(ctx, userID, eventID, reminders); err != nil {
		return nil, fmt.Errorf("set reminders: %w", err)
	}
	if err := s.TouchChangeSeq(ctx, eventID); err != nil {
		return nil, fmt.Errorf("bump change seq: %w", err)
	}
	return reminders, nil
}

// txRepos is the set of transaction-bound repositories a withTx body writes
// through. Grouped into one struct so a body names only what it needs — most
// use two or three of the eight. attachments is writeSeries' alone — it rows
// a SeriesWrite's already-on-disk Attachments (ADR-0040) inside the same
// transaction as their Master, so a failed write leaves no attachment row
// even though the bytes were saved beforehand (#135). attendees, groups,
// users, and workspaces are AddGroupAttendee's and Create's staged-Attendee
// write's alone — reading membership, checking each member's Disabled flag
// and Workspace membership, and inserting one attendees row per target all
// inside the same transaction keeps the snapshot honest against a
// concurrent membership change, and means a failure partway through never
// leaves a partial expansion (#162, #187, ADR-0046, ADR-0055). Every repo
// call inside a withTx body must go through this tx-bound set, never the
// EventService's own pooled repos directly — the test DB caps the pool at
// one connection (db.OpenInMemory), so a pooled call made while the
// transaction holds that connection deadlocks waiting for a connection the
// transaction itself is holding.
type txRepos struct {
	events        *repository.EventRepository
	exceptions    *repository.EventExceptionRepository
	reminders     *repository.EventReminderRepository
	sync          *repository.SyncRepository
	attachments   *repository.AttachmentRepository
	attendees     *repository.AttendeeRepository
	groups        *repository.GroupRepository
	users         *repository.UserRepository
	workspaces    *repository.WorkspaceRepository
	notifications *repository.NotificationRepository
	// outbox is nil when EventService.outbox is nil (no SMTP configured) —
	// inviteUser and expandGroupMembers check for that rather than calling
	// through a nil repository.
	outbox *repository.OutboxRepository
}

// withTx runs fn inside a transaction, passing it transaction-bound clones
// of the Event, EventException, EventReminder, Sync, Attachment, Attendee,
// Group, User, Workspace, Notification, and (when configured) Outbox
// repositories so a multi-table write — including its change_seq bump —
// commits or rolls back atomically. Reads and validation belong outside fn,
// before withTx is called (ADR-0018).
func (s *EventService) withTx(ctx context.Context, fn func(repos txRepos) error) error {
	return repository.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		repos := txRepos{
			events:        s.events.WithTx(tx),
			exceptions:    s.exceptions.WithTx(tx),
			reminders:     s.reminders.WithTx(tx),
			sync:          s.sync.WithTx(tx),
			attachments:   s.attachments.WithTx(tx),
			attendees:     s.attendees.WithTx(tx),
			groups:        s.groups.WithTx(tx),
			users:         s.users.WithTx(tx),
			workspaces:    s.workspaces.WithTx(tx),
			notifications: s.notifications.WithTx(tx),
		}
		if s.outbox != nil {
			repos.outbox = s.outbox.WithTx(tx)
		}
		return fn(repos)
	})
}

// EventWrite is an Event's writable fields, gathered into one value the same
// way SeriesWrite already gathers a whole series' — so Create and Update take
// four arguments instead of thirteen.
//
// ParentID and RecurrenceID anchor an Override to the Occurrence it replaces
// and are fixed when it is created: Create reads them, Update ignores them.
type EventWrite struct {
	CalendarID   string
	Title        string
	Start, End   time.Time
	AllDay       bool
	Rrule        string
	ParentID     *string
	RecurrenceID *time.Time
	Tzid         *string
	Description  string
	Location     string
	// URL is this Event's optional link (ADR-0063), stored exactly as
	// submitted alongside Description/Location — never validated or
	// rewritten here.
	URL string
	// Color is this Event's own color override — same Editor Access rule as
	// title or time (ADR-0034), nullable to mean "inherit the Calendar's
	// color" (ADR-0043). A caller resetting to the Calendar's color passes
	// nil, never the Calendar's current hex — see ResolveColor's contract in
	// docs/adr/0043-per-event-color-override.md.
	Color *string
	// AttendeeUserIDs, AttendeeGroupIDs, and AttendeeEmails are Attendees to
	// invite as part of this create, written inside the same transaction as
	// the Event itself (#187, ADR-0055, #200/ADR-0058). Explicit targets are
	// strict — an unknown User, a target outside the Calendar's Workspace, a
	// bad Group id, or a malformed/organizer/disabled-member address fails
	// the whole create — while each named Group's member expansion stays
	// lenient, exactly like AddGroupAttendee (ADR-0046). All three are nil
	// on every write path other than Create; Update carries no Attendee
	// fields of its own.
	AttendeeUserIDs  []int64
	AttendeeGroupIDs []int64
	AttendeeEmails   []string
	// CopyRemindersFrom, when set, copies every User's Reminder rows from
	// that Event onto the one Create mints, inside the same transaction
	// (ADR-0064): ParentID for a fresh Override (its Master's Reminder set),
	// or the old Master's id for a "This and following" split's new Master —
	// so a User whose Reminder applied to the old row's future Occurrences
	// doesn't silently lose it. Nil on every other Create.
	CopyRemindersFrom *string
}

// fields projects the write onto the columns the repository stores, dropping
// the Reminders that live in their own table.
func (w EventWrite) fields() repository.EventFields {
	return repository.EventFields{
		CalendarID:   w.CalendarID,
		Title:        w.Title,
		Start:        w.Start,
		End:          w.End,
		AllDay:       w.AllDay,
		Rrule:        w.Rrule,
		ParentID:     w.ParentID,
		RecurrenceID: w.RecurrenceID,
		Tzid:         w.Tzid,
		Description:  w.Description,
		Location:     w.Location,
		URL:          w.URL,
		Color:        w.Color,
	}
}

func (s *EventService) Create(ctx context.Context, userID int64, id string, write EventWrite) (repository.Event, error) {
	write.Title = strings.TrimSpace(write.Title)
	if write.Title == "" {
		return repository.Event{}, ErrInvalidTitle
	}
	if !write.End.After(write.Start) {
		return repository.Event{}, ErrInvalidTimeRange
	}
	normalizedColor, err := normalizeEventColor(write.Color)
	if err != nil {
		return repository.Event{}, err
	}
	write.Color = normalizedColor

	if write.ParentID != nil {
		if overrideCarriesOwnRrule(write.ParentID, write.Rrule) || write.RecurrenceID == nil {
			return repository.Event{}, ErrInvalidOverride
		}
		parent, err := s.getOwnedEvent(ctx, userID, *write.ParentID)
		if err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				return repository.Event{}, ErrParentNotFound
			}
			return repository.Event{}, err
		}
		if parent.ParentID != nil {
			return repository.Event{}, ErrParentIsOverride
		}
	} else if !isValidRecurrenceRule(write.Rrule) {
		return repository.Event{}, ErrInvalidRecurrenceRule
	}

	if err := s.requireWritableCalendar(ctx, userID, write.CalendarID); err != nil {
		return repository.Event{}, err
	}

	// Resolved once, outside the transaction, only when there's an Attendee
	// to check against it — every other Create stays exactly as cheap as it
	// was before #187.
	var workspaceID int64
	if len(write.AttendeeUserIDs) > 0 || len(write.AttendeeGroupIDs) > 0 || len(write.AttendeeEmails) > 0 {
		calendar, err := s.calendarByID(ctx, userID, write.CalendarID)
		if err != nil {
			return repository.Event{}, err
		}
		workspaceID = calendar.WorkspaceID
	}

	var event repository.Event
	err = s.withTx(ctx, func(repos txRepos) error {
		seq, err := repos.sync.NextChangeSeq(ctx)
		if err != nil {
			return err
		}
		e, err := repos.events.Create(ctx, id, &userID, write.fields(), seq)
		if err != nil {
			return err
		}
		// An Override copies its Master's whole Reminder set (AC6); a "This
		// and following" split's new Master copies the old Master's, named
		// explicitly via CopyRemindersFrom since it isn't a ParentID
		// relationship. The two are mutually exclusive in practice — an
		// Override always sets ParentID and never CopyRemindersFrom.
		copyFrom := write.ParentID
		if copyFrom == nil {
			copyFrom = write.CopyRemindersFrom
		}
		if copyFrom != nil {
			if err := repos.reminders.CopyByEventID(ctx, *copyFrom, e.ID); err != nil {
				return fmt.Errorf("copy reminders: %w", err)
			}
		}
		if err := s.addCreateAttendees(ctx, repos, e, workspaceID, write.AttendeeUserIDs, write.AttendeeGroupIDs, write.AttendeeEmails, userID); err != nil {
			return err
		}
		// Creating an Override changes its Master's calendar object (a new
		// VEVENT joins the series), so the Master's own change_seq must bump
		// too — it, not the Override, is what CalDAV reports (ADR-0025).
		if write.ParentID != nil {
			if err := repos.events.SetChangeSeq(ctx, *write.ParentID, seq); err != nil {
				return fmt.Errorf("bump parent change_seq: %w", err)
			}
		}
		event = e
		return nil
	})
	if err != nil {
		return repository.Event{}, fmt.Errorf("create event: %w", err)
	}
	// userID's own Reminders on the new row — empty for a plain create, or
	// whatever CopyRemindersFrom just copied them from, if any (ADR-0064).
	byEvent, err := s.reminders.ListByEventIDs(ctx, userID, []string{event.ID})
	if err != nil {
		return repository.Event{}, fmt.Errorf("read reminders: %w", err)
	}
	event.Reminders = byEvent[event.ID]
	result := []repository.Event{event}
	if err := s.attachCreatedByNames(ctx, result); err != nil {
		return repository.Event{}, err
	}
	if err := s.attachAttachments(ctx, result); err != nil {
		return repository.Event{}, err
	}
	if err := s.attachAttendeeCounts(ctx, result); err != nil {
		return repository.Event{}, err
	}
	return result[0], nil
}

// addCreateAttendees writes eventID's initial Attendees inside Create's own
// transaction (#187, ADR-0055), reusing inviteUser, expandGroupMembers, and
// inviteEmail — the same checks and the same Group-expansion loop
// AddAttendee, AddGroupAttendee, and AddAttendeeByEmail run, just against
// repos, the transaction-bound repositories, never the EventService's own
// pooled ones — a pooled call here would deadlock against the
// single-connection test database while the transaction holds its one
// connection (txRepos' doc comment).
//
// userIDs are explicit targets and strict, matching inviteUser's own
// checks: an unknown or Disabled User, or one outside workspaceID, fails
// eventID's entire create — the first invalid target found, not collected.
// groupIDs are explicit too and just as strict about the Group id itself —
// unknown, or belonging to another Workspace, fails the create — but each
// valid Group's member expansion stays lenient (expandGroupMembers' own
// policy), since the caller named the Group, not its individual members. A
// User named both individually and via a Group is silently deduplicated,
// same as AddGroupAttendee already tolerates re-inviting someone. emails are
// explicit and strict too, matching inviteEmail's own checks (ADR-0058,
// #200): malformed, naming the Organizer, or naming a Disabled Member all
// fail the create. actorUserID is the creating User — every Attendee this
// call queues an Invitation for is charged against their own hourly
// ceiling (#204, ADR-0058).
func (s *EventService) addCreateAttendees(ctx context.Context, repos txRepos, event repository.Event, workspaceID int64, userIDs, groupIDs []int64, emails []string, actorUserID int64) error {
	for _, targetUserID := range userIDs {
		if _, err := inviteUser(ctx, repos.users, repos.workspaces, repos.attendees, repos.notifications, repos.outbox, event, workspaceID, targetUserID, actorUserID, s.inviteRateLimitPerHour); err != nil {
			return err
		}
	}

	for _, groupID := range groupIDs {
		group, err := repos.groups.GetByID(ctx, groupID)
		if err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				return ErrGroupNotFound
			}
			return fmt.Errorf("look up attendee group: %w", err)
		}
		if group.WorkspaceID != workspaceID {
			return ErrAttendeeTargetNotInWorkspace
		}

		if _, err := expandGroupMembers(ctx, repos.groups, repos.users, repos.attendees, repos.notifications, repos.outbox, event, group.ID, actorUserID, s.inviteRateLimitPerHour); err != nil {
			return err
		}
	}

	for _, email := range emails {
		if _, err := inviteEmail(ctx, repos.users, repos.workspaces, repos.attendees, repos.notifications, repos.outbox, event, workspaceID, email, actorUserID, s.inviteRateLimitPerHour); err != nil {
			return err
		}
	}

	return nil
}

// overrideCarriesOwnRrule reports whether a write to parentID names an
// Override that also carries a non-empty rrule — a combination Create and
// Update both reject, since an Override is a complete standalone instance,
// never itself a Master (ADR-0016).
func overrideCarriesOwnRrule(parentID *string, rrule string) bool {
	return parentID != nil && rrule != ""
}

// isValidRecurrenceRule is the backend's light sanity check on an rrule. An
// empty rule means the event does not recur. A non-empty rule must at least name
// a frequency; anything more is the frontend's responsibility (ADR-0016).
func isValidRecurrenceRule(rrule string) bool {
	if rrule == "" {
		return true
	}
	return strings.Contains(rrule, "FREQ=")
}

// samePattern reports whether two rrules describe the same repetition
// pattern, ignoring their end condition (UNTIL/COUNT). A Master's rule
// "changing" only invalidates existing Overrides/Exceptions — forcing "All
// events" and discarding them — when the pattern itself changes; truncating
// or extending the end date (the "This and following" split, or a shortened
// custom recurrence) does not, since occurrences before the new end date are
// still generated the same way (ADR-0016).
func samePattern(a, b string) bool {
	return stripEndCondition(a) == stripEndCondition(b)
}

func stripEndCondition(rrule string) string {
	parts := strings.Split(rrule, ";")
	kept := parts[:0]
	for _, part := range parts {
		if strings.HasPrefix(part, "UNTIL=") || strings.HasPrefix(part, "COUNT=") {
			continue
		}
		kept = append(kept, part)
	}
	return strings.Join(kept, ";")
}

// List returns a user's events — every Event on a Calendar they have Access
// to, unioned with every Event they're an Attendee of regardless of Calendar
// Access (ADR-0046, #161) — with each Master's Exdates populated from its
// Exceptions (ADR-0016), and each event's Reminders populated from
// event_reminders (ADR-0020). Overrides always have an empty Exdates.
func (s *EventService) List(ctx context.Context, userID int64, from, to *time.Time) ([]repository.Event, error) {
	calendars, err := s.calendars.ListAccessible(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list calendars: %w", err)
	}
	calendarIDs := make([]string, len(calendars))
	for i, c := range calendars {
		calendarIDs[i] = c.ID
	}

	events, err := s.events.ListByCalendarIDs(ctx, calendarIDs, from, to)
	if err != nil {
		return nil, fmt.Errorf("list events: %w", err)
	}

	attendeeEvents, err := s.events.ListByAttendeeUserID(ctx, userID, from, to)
	if err != nil {
		return nil, fmt.Errorf("list attendee events: %w", err)
	}
	events = mergeEventsByID(events, attendeeEvents)

	knownCalendars := make(map[string]CalendarWithAccess, len(calendars))
	for _, c := range calendars {
		knownCalendars[c.ID] = c
	}
	if err := s.attachCalendarMeta(ctx, events, knownCalendars); err != nil {
		return nil, err
	}
	if err := s.attachExdates(ctx, events); err != nil {
		return nil, err
	}
	if err := s.attachViewerReminders(ctx, events, userID); err != nil {
		return nil, err
	}
	if err := s.attachCreatedByNames(ctx, events); err != nil {
		return nil, err
	}
	if err := s.attachAttachments(ctx, events); err != nil {
		return nil, err
	}
	if err := s.attachAttendeeCounts(ctx, events); err != nil {
		return nil, err
	}
	return events, nil
}

// attachCalendarMeta stamps each Event's CalendarName/CalendarColor
// (display-only, mirroring attachCreatedByNames) from known — the caller's
// own Calendar-Access set List already loaded — falling back to a single
// batched, access-unchecked Calendar lookup for whichever Events' Calendars
// aren't in known: an Attendee-only Event (ADR-0046), whose caller has no
// Access row to have populated known with in the first place. That fallback
// performs no visibility check of its own; it's safe only because every
// caller of List/Get has already established visibility into the Event
// itself.
func (s *EventService) attachCalendarMeta(ctx context.Context, events []repository.Event, known map[string]CalendarWithAccess) error {
	resolved := make(map[string]CalendarMeta, len(known))
	for id, c := range known {
		resolved[id] = CalendarMeta{Name: c.Name, Color: c.Color}
	}

	var missing []string
	seen := make(map[string]bool)
	for _, e := range events {
		if _, ok := resolved[e.CalendarID]; ok || seen[e.CalendarID] {
			continue
		}
		seen[e.CalendarID] = true
		missing = append(missing, e.CalendarID)
	}
	if len(missing) > 0 {
		fetched, err := s.calendars.AttendeeCalendarMetaByIDs(ctx, missing)
		if err != nil {
			return fmt.Errorf("resolve calendar meta: %w", err)
		}
		for id, meta := range fetched {
			resolved[id] = meta
		}
	}

	for i := range events {
		meta := resolved[events[i].CalendarID]
		events[i].CalendarName = meta.Name
		events[i].CalendarColor = meta.Color
	}
	return nil
}

// ListAllWithReminders returns every Event that carries at least one
// Reminder, across every Calendar, each paired with every User's own
// Reminders on it — the firing engine's read path (ADR-0021, ADR-0064).
func (s *EventService) ListAllWithReminders(ctx context.Context) ([]repository.EventWithOwner, error) {
	events, err := s.events.ListAllWithReminders(ctx)
	if err != nil {
		return nil, fmt.Errorf("list events with reminders: %w", err)
	}

	pointers := make([]*repository.Event, len(events))
	for i := range events {
		pointers[i] = &events[i].Event
	}
	if err := s.attachExdatesTo(ctx, pointers); err != nil {
		return nil, err
	}
	return events, nil
}

// mergeEventsByID appends b's Events not already present in a (by id) —
// List's Calendar-Access and Attendee halves can overlap when a caller has
// both to the same Event (ADR-0046), and the merged result must carry each
// Event exactly once.
func mergeEventsByID(a, b []repository.Event) []repository.Event {
	seen := make(map[string]bool, len(a))
	for _, e := range a {
		seen[e.ID] = true
	}

	merged := a
	for _, e := range b {
		if seen[e.ID] {
			continue
		}
		seen[e.ID] = true
		merged = append(merged, e)
	}
	return merged
}

// eventIDs collects the ids to hand a batched lookup, so a read of any size
// costs one query rather than one per Event.
func eventIDs(events []repository.Event) []string {
	ids := make([]string, 0, len(events))
	for _, e := range events {
		ids = append(ids, e.ID)
	}
	return ids
}

func (s *EventService) attachExdates(ctx context.Context, events []repository.Event) error {
	pointers := make([]*repository.Event, len(events))
	for i := range events {
		pointers[i] = &events[i]
	}
	return s.attachExdatesTo(ctx, pointers)
}

// attachExdatesTo fills in Exdates on rows that need not be contiguous in
// memory, mirroring attachViewerRemindersTo.
func (s *EventService) attachExdatesTo(ctx context.Context, events []*repository.Event) error {
	ids := make([]string, len(events))
	for i, e := range events {
		ids[i] = e.ID
	}

	exceptionsByParent, err := s.exceptions.ListByParentIDs(ctx, ids)
	if err != nil {
		return fmt.Errorf("list exceptions: %w", err)
	}

	for _, e := range events {
		e.Exdates = exceptionsByParent[e.ID]
	}
	return nil
}

// attachCreatedByNames fills in CreatedByName on events whose
// CreatedBy is set (#118), batching one query across every distinct creator
// rather than one per Event — the same shape as attachViewerReminders and
// attachExdates. An Event whose creator has since been deleted, or with no
// recorded creator at all, is simply left with an empty
// CreatedByName.
func (s *EventService) attachCreatedByNames(ctx context.Context, events []repository.Event) error {
	ids := make([]int64, 0, len(events))
	seen := make(map[int64]bool)
	for _, e := range events {
		if e.CreatedBy == nil || seen[*e.CreatedBy] {
			continue
		}
		seen[*e.CreatedBy] = true
		ids = append(ids, *e.CreatedBy)
	}

	users, err := s.users.GetByIDs(ctx, ids)
	if err != nil {
		return fmt.Errorf("list creators: %w", err)
	}

	for i := range events {
		if events[i].CreatedBy == nil {
			continue
		}
		events[i].CreatedByName = users[*events[i].CreatedBy].Name
	}
	return nil
}

// attachAttendeeCounts fills in AttendeeCount on every Event, batching one
// query across all of them rather than one per Event — the same shape as
// attachCreatedByNames. Unlike attachAttachments, this is not restricted to
// Masters: an Override can carry its own Attendees, and #193's
// progressive-disclosure rule needs an accurate count on whichever Event it
// was handed.
func (s *EventService) attachAttendeeCounts(ctx context.Context, events []repository.Event) error {
	ids := eventIDs(events)

	userIDsByEvent, err := s.attendees.ListUserIDsByEventIDs(ctx, ids)
	if err != nil {
		return fmt.Errorf("list attendee counts: %w", err)
	}

	for i := range events {
		events[i].AttendeeCount = len(userIDsByEvent[events[i].ID])
	}
	return nil
}

// attachViewerReminders is attachViewerRemindersTo for a contiguous slice.
func (s *EventService) attachViewerReminders(ctx context.Context, events []repository.Event, viewerID int64) error {
	pointers := make([]*repository.Event, len(events))
	for i := range events {
		pointers[i] = &events[i]
	}
	return s.attachViewerRemindersTo(ctx, pointers, viewerID)
}

// attachViewerRemindersTo fills in Reminders on events with viewerID's own
// rows (ADR-0064) — every series read path's Reminder step, one User asking
// for a set of Events, so there is only ever one User to scope the lookup
// to, regardless of how many distinct Calendars/Owners events itself spans.
func (s *EventService) attachViewerRemindersTo(ctx context.Context, events []*repository.Event, viewerID int64) error {
	ids := make([]string, len(events))
	for i, e := range events {
		ids[i] = e.ID
	}

	remindersByEvent, err := s.reminders.ListByEventIDs(ctx, viewerID, ids)
	if err != nil {
		return fmt.Errorf("list reminders: %w", err)
	}
	for _, e := range events {
		e.Reminders = remindersByEvent[e.ID]
	}
	return nil
}

// attachAttachments fills in Attachments on each Master in events (an
// Override is left with none — it can never carry its own, ADR-0040),
// plus UploadedByName on each of them, batching one Attachment lookup
// and one user lookup across every Event rather than one per Event, the
// same shape as attachViewerReminders and attachCreatedByNames.
func (s *EventService) attachAttachments(ctx context.Context, events []repository.Event) error {
	masterIDs := make([]string, 0, len(events))
	for _, e := range events {
		if e.ParentID == nil {
			masterIDs = append(masterIDs, e.ID)
		}
	}

	attachmentsByEvent, err := s.attachments.ListByEventIDs(ctx, masterIDs)
	if err != nil {
		return fmt.Errorf("list attachments: %w", err)
	}

	uploaderIDs := make([]int64, 0)
	seen := make(map[int64]bool)
	for _, list := range attachmentsByEvent {
		for _, a := range list {
			if a.UploadedBy == nil || seen[*a.UploadedBy] {
				continue
			}
			seen[*a.UploadedBy] = true
			uploaderIDs = append(uploaderIDs, *a.UploadedBy)
		}
	}
	uploaders, err := s.users.GetByIDs(ctx, uploaderIDs)
	if err != nil {
		return fmt.Errorf("list uploaders: %w", err)
	}

	for i := range events {
		if events[i].ParentID != nil {
			continue
		}
		list := attachmentsByEvent[events[i].ID]
		for j := range list {
			if list[j].UploadedBy != nil {
				list[j].UploadedByName = uploaders[*list[j].UploadedBy].Name
			}
		}
		events[i].Attachments = list
	}
	return nil
}

// attachOrganizersAndAttendees is attachOrganizersAndAttendeesTo for a
// contiguous slice — the value-slice convenience wrapper hydrateSeries uses,
// mirroring attachViewerReminders/attachViewerRemindersTo's split.
func (s *EventService) attachOrganizersAndAttendees(ctx context.Context, events []repository.Event) error {
	pointers := make([]*repository.Event, len(events))
	for i := range events {
		pointers[i] = &events[i]
	}
	return s.attachOrganizersAndAttendeesTo(ctx, pointers)
}

// attachOrganizersAndAttendeesTo fills in each event's Attendees (with
// Name/Email) and its Organizer's CreatedByName/CreatedByEmail, batching one
// query across all of them rather than one per Event — the CalDAV/ICS
// codec's read path (ADR-0062), which needs the mailto addresses
// attachCreatedByNames and attachAttendeeCounts don't carry. Scoped per
// Event row like attachViewerRemindersTo: a Master and each of its Overrides can
// each carry their own Attendees and their own Organizer (#193, ADR-0055).
func (s *EventService) attachOrganizersAndAttendeesTo(ctx context.Context, events []*repository.Event) error {
	ids := make([]string, 0, len(events))
	for _, e := range events {
		ids = append(ids, e.ID)
	}
	attendeesByEvent, err := s.attendees.ListWithNamesByEventIDs(ctx, ids)
	if err != nil {
		return fmt.Errorf("list attendees: %w", err)
	}

	organizerIDs := make([]int64, 0)
	seen := make(map[int64]bool)
	for _, e := range events {
		if e.CreatedBy == nil || seen[*e.CreatedBy] {
			continue
		}
		seen[*e.CreatedBy] = true
		organizerIDs = append(organizerIDs, *e.CreatedBy)
	}
	organizers, err := s.users.GetByIDs(ctx, organizerIDs)
	if err != nil {
		return fmt.Errorf("list organizers: %w", err)
	}

	for _, e := range events {
		e.Attendees = attendeesByEvent[e.ID]
		if e.CreatedBy != nil {
			e.CreatedByName = organizers[*e.CreatedBy].Name
			e.CreatedByEmail = organizers[*e.CreatedBy].Email
		}
	}
	return nil
}

// Get returns id if userID can see it — via Calendar Access or, failing
// that, as one of its Attendees (ADR-0046, #161).
func (s *EventService) Get(ctx context.Context, userID int64, id string) (repository.Event, error) {
	event, err := s.getVisibleEvent(ctx, userID, id)
	if err != nil {
		return repository.Event{}, err
	}
	events := []repository.Event{event}
	if err := s.attachCalendarMeta(ctx, events, nil); err != nil {
		return repository.Event{}, err
	}
	if err := s.attachExdates(ctx, events); err != nil {
		return repository.Event{}, err
	}
	if err := s.attachViewerReminders(ctx, events, userID); err != nil {
		return repository.Event{}, err
	}
	if err := s.attachCreatedByNames(ctx, events); err != nil {
		return repository.Event{}, err
	}
	if err := s.attachAttachments(ctx, events); err != nil {
		return repository.Event{}, err
	}
	if err := s.attachAttendeeCounts(ctx, events); err != nil {
		return repository.Event{}, err
	}
	return events[0], nil
}

// Update rewrites id's fields. write.ParentID and write.RecurrenceID are
// ignored — see EventWrite. Re-sends id's Invitation to its own current
// Attendees whenever a field an Invitation renders actually changed,
// bumping its iTIP sequence only when the change is material — start, end,
// rrule, or all_day (ADR-0059, #201). A rule-pattern change forces
// "All events" and discards id's existing Overrides/Exceptions (ADR-0016);
// each discarded Override's own Attendees get a METHOD:CANCEL first, same
// as Delete, since discarding an Override is deleting an Event.
func (s *EventService) Update(ctx context.Context, userID int64, id string, write EventWrite) (repository.Event, error) {
	write.Title = strings.TrimSpace(write.Title)
	if write.Title == "" {
		return repository.Event{}, ErrInvalidTitle
	}
	if !write.End.After(write.Start) {
		return repository.Event{}, ErrInvalidTimeRange
	}
	if !isValidRecurrenceRule(write.Rrule) {
		return repository.Event{}, ErrInvalidRecurrenceRule
	}
	normalizedColor, err := normalizeEventColor(write.Color)
	if err != nil {
		return repository.Event{}, err
	}
	write.Color = normalizedColor
	if err := s.requireWritableCalendar(ctx, userID, write.CalendarID); err != nil {
		return repository.Event{}, err
	}

	existing, err := s.getOwnedEvent(ctx, userID, id)
	if err != nil {
		return repository.Event{}, err
	}
	// A moving Update targets write.CalendarID but still touches existing's
	// current row, so its source Calendar (if different) is guarded too —
	// otherwise editing an Event out of a Subscribed Calendar would be a
	// legitimate write, exactly the case ADR-0032 exists to prevent.
	if existing.CalendarID != write.CalendarID {
		if err := s.requireWritableCalendar(ctx, userID, existing.CalendarID); err != nil {
			return repository.Event{}, err
		}
	}

	if overrideCarriesOwnRrule(existing.ParentID, write.Rrule) {
		return repository.Event{}, ErrInvalidOverride
	}

	// A Master's rule changing (or being removed) is forced to "All events" and
	// discards existing Overrides/Exceptions, because their recurrence ids may
	// no longer be generated by the new rule (ADR-0016). The frontend warns
	// before calling this; a non-recurring Master or an unchanged rule has
	// nothing to discard.
	discardChildren := existing.ParentID == nil && !samePattern(existing.Rrule, write.Rrule)

	// material is the ADR-0059 change set that resets a recipient's PARTSTAT
	// — start, end, rrule, all_day — and is the only thing that bumps this
	// row's own iTIP sequence. contentChanged widens that to everything an
	// Invitation actually renders — title, description, location and colour
	// (ADR-0063) on top of the material set: any of it re-sends the
	// Invitation to every one of id's own current Attendees, bumped or not.
	material := materialFieldsChanged(existing, write)
	newSequence := existing.Sequence
	if material {
		newSequence++
	}
	contentChanged := material || existing.Title != write.Title || existing.Description != write.Description || existing.Location != write.Location || existing.URL != write.URL || !colorEqual(existing.Color, write.Color)

	var updated repository.Event
	err = s.withTx(ctx, func(repos txRepos) error {
		seq, err := repos.sync.NextChangeSeq(ctx)
		if err != nil {
			return err
		}
		u, err := repos.events.Update(ctx, id, write.fields(), seq, newSequence)
		if err != nil {
			return err
		}
		updated = u

		if discardChildren {
			// A discarded Override is a deleted Event exactly as much as one
			// removed through Delete — its own Attendees, if any, are owed
			// the same METHOD:CANCEL (ADR-0059, #201), captured before the
			// row disappears.
			discardedByParent, err := repos.events.ListChildrenByParentIDs(ctx, []string{id})
			if err != nil {
				return fmt.Errorf("list discarded overrides: %w", err)
			}
			for _, child := range discardedByParent[id] {
				if err := s.enqueueCancellationsForRow(ctx, repos, child); err != nil {
					return err
				}
			}
			if err := repos.events.DeleteChildrenOf(ctx, id); err != nil {
				return fmt.Errorf("discard overrides: %w", err)
			}
			if err := repos.exceptions.DeleteByParentID(ctx, id); err != nil {
				return fmt.Errorf("discard exceptions: %w", err)
			}
		}

		// Updating an Override changes its Master's calendar object, so the
		// Master's own change_seq must bump too (ADR-0025).
		if existing.ParentID != nil {
			if err := repos.events.SetChangeSeq(ctx, *existing.ParentID, seq); err != nil {
				return fmt.Errorf("bump parent change_seq: %w", err)
			}
		}

		if contentChanged {
			if err := s.enqueueReinvitations(ctx, repos, id, nil, nil); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return repository.Event{}, err
	}

	result := []repository.Event{updated}
	if err := s.attachExdates(ctx, result); err != nil {
		return repository.Event{}, err
	}
	if err := s.attachViewerReminders(ctx, result, userID); err != nil {
		return repository.Event{}, err
	}
	if err := s.attachCreatedByNames(ctx, result); err != nil {
		return repository.Event{}, err
	}
	if err := s.attachAttachments(ctx, result); err != nil {
		return repository.Event{}, err
	}
	if err := s.attachAttendeeCounts(ctx, result); err != nil {
		return repository.Event{}, err
	}
	return result[0], nil
}

// ImportSeries writes every series in writes as a brand-new Master (mints
// its own id) plus its Overrides and Exdates, in one transaction taking one
// change_seq (ADR-0030). It is ordinary ICS import's writer, and refuses a
// Subscribed Calendar as its target like every other mutating method
// (ADR-0032) — SubscribeService writes through ImportSubscribedSeries
// instead, since it targets its own Subscribed Calendar deliberately.
// Ordinary import leaves ExternalUID empty, discarding the foreign identity
// entirely; a Subscription's writer sets it so a later Refresh can
// reconcile by it (ADR-0033). Unlike PutSeries, this never updates an
// existing row — an import is always insert-only — and looping PutSeries
// once per series would mean N transactions, N sync bumps, and a
// half-imported Calendar if it died partway through; ImportSeries validates
// every write up front so a bad series in a large import fails before
// anything is written, not partway through the transaction. Returns the
// number of series written.
func (s *EventService) ImportSeries(ctx context.Context, userID int64, calendarID string, writes []SeriesWrite) (int, error) {
	if err := validateSeriesWrites(writes); err != nil {
		return 0, err
	}

	if err := s.requireWritableCalendar(ctx, userID, calendarID); err != nil {
		return 0, err
	}

	// An ICS import writes the importing User's Reminders (ADR-0064) — unlike
	// a Subscription's own fetched Events, which belong to the Calendar's
	// Owner (ImportSubscribedSeries below).
	return s.writeSeries(ctx, userID, calendarID, writes, userID)
}

// ImportSubscribedSeries is one of EventService's two bypasses of the
// Subscribed Calendar write guard (ADR-0032), alongside
// ReconcileSubscribedSeries (#85): otherwise identical to ImportSeries, it
// skips requireWritable because writing a Subscription's own fetched Events
// into its own Subscribed Calendar is a legitimate write. Its only caller is
// SubscribeService.Subscribe's initial import — a later Refresh writes
// through ReconcileSubscribedSeries instead, since it must update existing
// rows in place rather than always inserting. Every other write must go
// through ImportSeries or one of the guarded methods instead.
func (s *EventService) ImportSubscribedSeries(ctx context.Context, userID int64, calendarID string, writes []SeriesWrite) (int, error) {
	if err := validateSeriesWrites(writes); err != nil {
		return 0, err
	}

	if _, err := s.calendarByID(ctx, userID, calendarID); err != nil {
		return 0, err
	}

	// A Subscription's own fetched Reminders belong to the Calendar's Owner,
	// not the User who happened to trigger the Subscribe (ADR-0064) — "a
	// Source's alarms belong to the Calendar's Owner".
	ownerID, err := s.reminderOwnerID(ctx, calendarID)
	if err != nil {
		return 0, err
	}
	return s.writeSeries(ctx, userID, calendarID, writes, ownerID)
}

// writeSeries is ImportSeries and ImportSubscribedSeries' shared insert-only
// write, once each has resolved and (except for the bypass) guarded
// calendarID. reminderUserID names whose Reminders every write's Reminders
// land under — the importing User for a plain ICS import, the Calendar's
// Owner for a Subscription's own fetched Events (ADR-0064) — since the two
// callers disagree on this independently of who userID (the acting/creating
// User stamped on every row) is.
func (s *EventService) writeSeries(ctx context.Context, userID int64, calendarID string, writes []SeriesWrite, reminderUserID int64) (int, error) {
	err := s.withTx(ctx, func(repos txRepos) error {
		seq, err := repos.sync.NextChangeSeq(ctx)
		if err != nil {
			return err
		}

		for _, w := range writes {
			masterID := uuid.NewString()
			master := repository.EventFields{
				CalendarID:  calendarID,
				Title:       w.Title,
				Start:       w.Start,
				End:         w.End,
				AllDay:      w.AllDay,
				Rrule:       w.Rrule,
				Tzid:        w.Tzid,
				Description: w.Description,
				Location:    w.Location,
				URL:         w.URL,
				Color:       w.Color,
				ExternalUID: nonEmptyPtr(w.ExternalUID),
			}
			if _, err := repos.events.Create(ctx, masterID, &userID, master, seq); err != nil {
				return fmt.Errorf("create master: %w", err)
			}
			if err := repos.reminders.ReplaceByEventID(ctx, reminderUserID, masterID, w.Reminders); err != nil {
				return fmt.Errorf("persist master reminders: %w", err)
			}
			for _, exdate := range w.Exdates {
				if err := repos.exceptions.Add(ctx, masterID, exdate); err != nil {
					return fmt.Errorf("add exdate: %w", err)
				}
			}
			for _, a := range w.Attachments {
				if _, err := repos.attachments.Create(ctx, a.ID, masterID, &userID, a.Filename, a.ContentType, a.SizeBytes); err != nil {
					return fmt.Errorf("create attachment: %w", err)
				}
			}

			for _, o := range w.Overrides {
				override := repository.EventFields{
					CalendarID:   calendarID,
					Title:        o.Title,
					Start:        o.Start,
					End:          o.End,
					AllDay:       o.AllDay,
					Tzid:         o.Tzid,
					Description:  o.Description,
					Location:     o.Location,
					URL:          o.URL,
					Color:        o.Color,
					ParentID:     &masterID,
					RecurrenceID: &o.RecurrenceID,
					ExternalUID:  nonEmptyPtr(o.ExternalUID),
				}
				overrideID := uuid.NewString()
				if _, err := repos.events.Create(ctx, overrideID, &userID, override, seq); err != nil {
					return fmt.Errorf("create override: %w", err)
				}
				if err := repos.reminders.ReplaceByEventID(ctx, reminderUserID, overrideID, o.Reminders); err != nil {
					return fmt.Errorf("persist override reminders: %w", err)
				}
			}
		}
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("import series: %w", err)
	}

	return len(writes), nil
}

// nonEmptyPtr returns nil for "", else a pointer to s — for optional string
// fields (like ExternalUID) that repository.EventFields stores as *string.
func nonEmptyPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// ReconcileSummary is what a Refresh's DB apply actually did, for the
// caller to report (#85, ADR-0033).
type ReconcileSummary struct {
	Created    int
	Updated    int
	Tombstoned int
}

// ReconcileSubscribedSeries applies result — Refresh's already-decided plan
// (service.ReconcileSeries) — to calendarID inside one transaction: each
// upsert creates a new series (MasterID empty) or updates one in place
// (Overrides matched by RecurrenceID and Exdates replaced wholesale,
// exactly as PutSeries does for a CalDAV PUT), each tombstone deletes an
// existing series outright, cascading to its Overrides. One change_seq bump
// per series actually written; ReconcileSeries has already excluded
// unchanged series from result.Upserts, so those cost nothing here. This is
// ImportSubscribedSeries' sibling bypass of the Subscribed Calendar write
// guard (ADR-0032) — the only other legitimate writer of one, alongside
// Subscribe's initial import.
func (s *EventService) ReconcileSubscribedSeries(ctx context.Context, userID int64, calendarID string, result ReconcileResult) (ReconcileSummary, error) {
	for i, upsert := range result.Upserts {
		title, err := validateEventFields(upsert.Write.Title, upsert.Write.Start, upsert.Write.End, upsert.Write.Reminders)
		if err != nil {
			return ReconcileSummary{}, err
		}
		result.Upserts[i].Write.Title = title
		if !isValidRecurrenceRule(upsert.Write.Rrule) {
			return ReconcileSummary{}, ErrInvalidRecurrenceRule
		}
		for j, o := range upsert.Write.Overrides {
			trimmed, err := validateEventFields(o.Title, o.Start, o.End, o.Reminders)
			if err != nil {
				return ReconcileSummary{}, err
			}
			result.Upserts[i].Write.Overrides[j].Title = trimmed
		}
	}

	calendar, err := s.calendarByID(ctx, userID, calendarID)
	if err != nil {
		return ReconcileSummary{}, err
	}
	// Every Reminder write names the Calendar's Owner, not the reconciling
	// User (ADR-0064 step one, #208) — a Refresh's own KeepAlarms rows
	// belong to the Owner (ADR-0064's "A Source's alarms belong to the
	// Calendar's Owner").
	ownerID := calendar.UserID

	var summary ReconcileSummary
	err = s.withTx(ctx, func(repos txRepos) error {
		for _, upsert := range result.Upserts {
			if upsert.MasterID == "" {
				if err := s.createSubscribedSeries(ctx, repos, userID, ownerID, calendarID, upsert.Write); err != nil {
					return err
				}
				summary.Created++
				continue
			}
			if err := s.updateSubscribedSeries(ctx, repos, userID, ownerID, calendarID, upsert.MasterID, upsert.Write); err != nil {
				return err
			}
			summary.Updated++
		}

		for _, masterID := range result.Tombstones {
			if err := s.tombstoneSubscribedSeries(ctx, repos, calendarID, masterID); err != nil {
				return err
			}
			summary.Tombstoned++
		}

		return nil
	})
	if err != nil {
		return ReconcileSummary{}, fmt.Errorf("reconcile subscribed series: %w", err)
	}

	return summary, nil
}

// createSubscribedSeries mints a new Master (and its Overrides) for write,
// carrying its ExternalUID, taking one change_seq bump for the whole series.
// ownerID is calendarID's Owner — every Reminder write names it, not userID
// (ADR-0064 step one, #208).
func (s *EventService) createSubscribedSeries(ctx context.Context, repos txRepos, userID, ownerID int64, calendarID string, write SeriesWrite) error {
	seq, err := repos.sync.NextChangeSeq(ctx)
	if err != nil {
		return err
	}

	masterID := uuid.NewString()
	master := repository.EventFields{
		CalendarID:  calendarID,
		Title:       write.Title,
		Start:       write.Start,
		End:         write.End,
		AllDay:      write.AllDay,
		Rrule:       write.Rrule,
		Tzid:        write.Tzid,
		Description: write.Description,
		Location:    write.Location,
		URL:         write.URL,
		ExternalUID: nonEmptyPtr(write.ExternalUID),
	}
	if _, err := repos.events.Create(ctx, masterID, &userID, master, seq); err != nil {
		return fmt.Errorf("create master: %w", err)
	}
	if err := repos.reminders.ReplaceByEventID(ctx, ownerID, masterID, write.Reminders); err != nil {
		return fmt.Errorf("persist master reminders: %w", err)
	}
	for _, exdate := range write.Exdates {
		if err := repos.exceptions.Add(ctx, masterID, exdate); err != nil {
			return fmt.Errorf("add exdate: %w", err)
		}
	}

	for _, o := range write.Overrides {
		override := repository.EventFields{
			CalendarID:   calendarID,
			Title:        o.Title,
			Start:        o.Start,
			End:          o.End,
			AllDay:       o.AllDay,
			Tzid:         o.Tzid,
			Description:  o.Description,
			Location:     o.Location,
			URL:          o.URL,
			ParentID:     &masterID,
			RecurrenceID: &o.RecurrenceID,
			ExternalUID:  nonEmptyPtr(o.ExternalUID),
		}
		overrideID := uuid.NewString()
		if _, err := repos.events.Create(ctx, overrideID, &userID, override, seq); err != nil {
			return fmt.Errorf("create override: %w", err)
		}
		if err := repos.reminders.ReplaceByEventID(ctx, ownerID, overrideID, o.Reminders); err != nil {
			return fmt.Errorf("persist override reminders: %w", err)
		}
	}
	return nil
}

// updateSubscribedSeries updates masterID's row and its Overrides in place
// from write — the same match-by-RecurrenceID, replace-Exdates-wholesale
// shape PutSeries uses for a CalDAV PUT — taking one change_seq bump for the
// whole series. masterID's own ExternalUID is never touched (it is set on
// insert only); Overrides created here inherit it from write.ExternalUID,
// exactly as createSubscribedSeries does. ownerID is calendarID's Owner —
// every Reminder write names it, not userID (ADR-0064 step one, #208).
func (s *EventService) updateSubscribedSeries(ctx context.Context, repos txRepos, userID, ownerID int64, calendarID, masterID string, write SeriesWrite) error {
	existingMaster, err := repos.events.GetByID(ctx, masterID)
	if err != nil {
		return fmt.Errorf("get existing master: %w", err)
	}
	existingOverrides, err := repos.events.ListChildrenByParentIDs(ctx, []string{masterID})
	if err != nil {
		return fmt.Errorf("list existing overrides: %w", err)
	}
	existingByRecurrenceID := make(map[int64]repository.Event, len(existingOverrides[masterID]))
	for _, o := range existingOverrides[masterID] {
		existingByRecurrenceID[o.RecurrenceID.UnixNano()] = o
	}

	seq, err := repos.sync.NextChangeSeq(ctx)
	if err != nil {
		return err
	}

	master := repository.EventFields{
		CalendarID:  calendarID,
		Title:       write.Title,
		Start:       write.Start,
		End:         write.End,
		AllDay:      write.AllDay,
		Rrule:       write.Rrule,
		Tzid:        write.Tzid,
		Description: write.Description,
		Location:    write.Location,
		URL:         write.URL,
	}
	// A Subscription's Refresh reconcile carries no iTIP lifecycle of its own
	// (a Subscribed Calendar's Events carry no Attendees, ADR-0032/ADR-0059)
	// — sequence is simply preserved rather than material-change detected.
	if _, err := repos.events.Update(ctx, masterID, master, seq, existingMaster.Sequence); err != nil {
		return fmt.Errorf("update master: %w", err)
	}
	if err := repos.reminders.ReplaceByEventID(ctx, ownerID, masterID, write.Reminders); err != nil {
		return fmt.Errorf("persist master reminders: %w", err)
	}

	if err := repos.exceptions.DeleteByParentID(ctx, masterID); err != nil {
		return fmt.Errorf("clear exdates: %w", err)
	}
	for _, exdate := range write.Exdates {
		if err := repos.exceptions.Add(ctx, masterID, exdate); err != nil {
			return fmt.Errorf("add exdate: %w", err)
		}
	}

	seen := make(map[int64]bool, len(write.Overrides))
	for _, o := range write.Overrides {
		key := o.RecurrenceID.UnixNano()
		seen[key] = true

		override := repository.EventFields{
			CalendarID:  calendarID,
			Title:       o.Title,
			Start:       o.Start,
			End:         o.End,
			AllDay:      o.AllDay,
			Tzid:        o.Tzid,
			Description: o.Description,
			Location:    o.Location,
			URL:         o.URL,
		}

		if existing, ok := existingByRecurrenceID[key]; ok {
			if _, err := repos.events.Update(ctx, existing.ID, override, seq, existing.Sequence); err != nil {
				return fmt.Errorf("update override: %w", err)
			}
			if err := repos.reminders.ReplaceByEventID(ctx, ownerID, existing.ID, o.Reminders); err != nil {
				return fmt.Errorf("persist override reminders: %w", err)
			}
			continue
		}

		override.ParentID = &masterID
		override.RecurrenceID = &o.RecurrenceID
		override.ExternalUID = nonEmptyPtr(o.ExternalUID)

		overrideID := uuid.NewString()
		if _, err := repos.events.Create(ctx, overrideID, &userID, override, seq); err != nil {
			return fmt.Errorf("create override: %w", err)
		}
		if err := repos.reminders.ReplaceByEventID(ctx, ownerID, overrideID, o.Reminders); err != nil {
			return fmt.Errorf("persist override reminders: %w", err)
		}
	}

	for key, existing := range existingByRecurrenceID {
		if seen[key] {
			continue
		}
		if err := repos.events.Delete(ctx, existing.ID); err != nil {
			return fmt.Errorf("delete removed override: %w", err)
		}
	}

	return nil
}

// tombstoneSubscribedSeries deletes masterID outright (cascading to its
// Overrides via the events table's foreign key) and records its removal as
// a tombstone under calendarID, one change_seq bump — mirroring Delete's
// Master case, but bypassing the Subscribed Calendar write guard the way
// every Refresh write does (ADR-0032).
func (s *EventService) tombstoneSubscribedSeries(ctx context.Context, repos txRepos, calendarID, masterID string) error {
	if err := repos.events.Delete(ctx, masterID); err != nil {
		return fmt.Errorf("delete tombstoned series: %w", err)
	}

	seq, err := repos.sync.NextChangeSeq(ctx)
	if err != nil {
		return err
	}

	return repos.sync.Tombstone(ctx, calendarID, masterID, seq)
}

// ClearSubscribedCalendarReminders deletes the Calendar Owner's Reminders on
// calendarID's Events — Masters and Overrides alike — the immediate
// consequence of a Subscription's KeepAlarms being turned off (#87,
// ADR-0032): "off" must mean no Reminders exist, not merely that a future
// Refresh will stop adding them. Scoped to the Owner (ADR-0064 step one,
// #208) since a Source's alarms belong to them alone. Bumps each Master's
// change_seq once so a CalDAV client sees the alarm-less object on its next
// sync, mirroring how an Override's own change is reflected on its Master
// (ADR-0025). This is ImportSubscribedSeries and ReconcileSubscribedSeries'
// third sibling bypass of the Subscribed Calendar write guard (ADR-0032) —
// clearing a Subscription's own Reminders in its own Subscribed Calendar is
// a legitimate write.
func (s *EventService) ClearSubscribedCalendarReminders(ctx context.Context, userID int64, calendarID string) error {
	calendar, err := s.calendarByID(ctx, userID, calendarID)
	if err != nil {
		return err
	}
	ownerID := calendar.UserID

	masters, overridesByParent, err := s.ListSeriesByCalendar(ctx, userID, calendarID)
	if err != nil {
		return err
	}

	return s.withTx(ctx, func(repos txRepos) error {
		for _, m := range masters {
			seq, err := repos.sync.NextChangeSeq(ctx)
			if err != nil {
				return err
			}
			if err := repos.reminders.ReplaceByEventID(ctx, ownerID, m.ID, nil); err != nil {
				return fmt.Errorf("clear master reminders: %w", err)
			}
			for _, o := range overridesByParent[m.ID] {
				if err := repos.reminders.ReplaceByEventID(ctx, ownerID, o.ID, nil); err != nil {
					return fmt.Errorf("clear override reminders: %w", err)
				}
			}
			if err := repos.events.SetChangeSeq(ctx, m.ID, seq); err != nil {
				return fmt.Errorf("bump master change_seq: %w", err)
			}
		}
		return nil
	})
}

// GetSeries returns masterID's Master (with Exdates and Reminders attached)
// and its Overrides (each with its own Reminders attached), for CalDAV's
// {masterId}.ics resource (ADR-0025). Only a Master is independently
// addressable, so masterID naming an Override returns ErrParentIsOverride.
func (s *EventService) GetSeries(ctx context.Context, userID int64, masterID string) (repository.Event, []repository.Event, error) {
	master, err := s.getOwnedEvent(ctx, userID, masterID)
	if err != nil {
		return repository.Event{}, nil, err
	}
	if master.ParentID != nil {
		return repository.Event{}, nil, ErrParentIsOverride
	}

	overridesByParent, err := s.events.ListChildrenByParentIDs(ctx, []string{masterID})
	if err != nil {
		return repository.Event{}, nil, fmt.Errorf("list overrides: %w", err)
	}

	return s.hydrateSeries(ctx, userID, master, overridesByParent[masterID])
}

// hydrateSeries attaches Exdates to master, then Reminders, Attachments, and
// each row's own Attendees/Organizer to master and overrides together,
// returning master then overrides — the tail GetSeries and
// GetAttendeeOnlySeries share once each has resolved masterID and confirmed
// the caller's own visibility to it (Calendar Access, or Attendee —
// ADR-0046). userID is whoever is asking, so Reminders resolve to their own
// rows, not the Calendar's Owner's (ADR-0064).
func (s *EventService) hydrateSeries(ctx context.Context, userID int64, master repository.Event, overrides []repository.Event) (repository.Event, []repository.Event, error) {
	masters := []repository.Event{master}
	if err := s.attachExdates(ctx, masters); err != nil {
		return repository.Event{}, nil, err
	}
	master = masters[0]

	all := append([]repository.Event{master}, overrides...)
	if err := s.attachViewerReminders(ctx, all, userID); err != nil {
		return repository.Event{}, nil, err
	}
	if err := s.attachAttachments(ctx, all); err != nil {
		return repository.Event{}, nil, err
	}
	if err := s.attachOrganizersAndAttendees(ctx, all); err != nil {
		return repository.Event{}, nil, err
	}

	return all[0], all[1:], nil
}

// GetSeriesForEvent is GetSeries generalized to accept id naming either a
// Master or one of its Overrides — both resolve to the same series, since a
// caller addressing an Occurrence often only knows the Override id it fired
// against, not its parent's (#76).
func (s *EventService) GetSeriesForEvent(ctx context.Context, userID int64, id string) (repository.Event, []repository.Event, error) {
	event, err := s.getOwnedEvent(ctx, userID, id)
	if err != nil {
		return repository.Event{}, nil, err
	}

	masterID := id
	if event.ParentID != nil {
		masterID = *event.ParentID
	}
	return s.GetSeries(ctx, userID, masterID)
}

// GetOccurrence returns id's series flattened to the single Occurrence
// starting at occurrenceStart, for ICS export's scope=occurrence (#76): the
// matching Override's own fields verbatim if one exists at that
// RecurrenceID, or the Master's own fields with Start/End shifted to
// occurrenceStart/occurrenceStart+duration otherwise. id may name either a
// Master or an Override (see GetSeriesForEvent). The returned Event always
// carries a cleared Rrule/ParentID/RecurrenceID — it describes one concrete
// Occurrence, not a series or a series member.
func (s *EventService) GetOccurrence(ctx context.Context, userID int64, id string, occurrenceStart time.Time) (repository.Event, error) {
	master, overrides, err := s.GetSeriesForEvent(ctx, userID, id)
	if err != nil {
		return repository.Event{}, err
	}

	if override, ok := findOverrideForOccurrence(overrides, occurrenceStart); ok {
		flattened := override
		flattened.ParentID = nil
		flattened.RecurrenceID = nil
		// An Override never carries a rule of its own (ADR-0016), so
		// this is always already "", but cleared explicitly since
		// icalendar.OccurrenceToICal trusts its caller to have done so.
		flattened.Rrule = ""
		return flattened, nil
	}

	valid, err := isOccurrenceStart(master, occurrenceStart)
	if err != nil {
		return repository.Event{}, err
	}
	if !valid {
		return repository.Event{}, ErrOccurrenceNotFound
	}

	flattened := master
	flattened.Start = occurrenceStart
	flattened.End = occurrenceStart.Add(master.End.Sub(master.Start))
	flattened.Rrule = ""
	flattened.Exdates = nil
	return flattened, nil
}

// findOverrideForOccurrence returns the Override in overrides that replaces
// occurrenceStart, if any — the "does this Occurrence have an Override"
// lookup GetOccurrence and resolveReplyEvent both need, kept in one place so
// the two don't drift apart on what counts as a match.
func findOverrideForOccurrence(overrides []repository.Event, occurrenceStart time.Time) (repository.Event, bool) {
	for _, override := range overrides {
		if override.RecurrenceID != nil && override.RecurrenceID.Equal(occurrenceStart) {
			return override, true
		}
	}
	return repository.Event{}, false
}

// isOccurrenceStart reports whether occurrenceStart is a real, non-excepted
// Occurrence of master's series: master's own start if it doesn't recur, or
// one of the rrule's generated starts otherwise — reusing the same Go
// expander CalDAV's time-range query uses (recurrence.ExpandOccurrences)
// rather than adding a third implementation (see the note on #74).
func isOccurrenceStart(master repository.Event, occurrenceStart time.Time) (bool, error) {
	for _, exdate := range master.Exdates {
		if exdate.Equal(occurrenceStart) {
			return false, nil
		}
	}

	if master.Rrule == "" {
		return master.Start.Equal(occurrenceStart), nil
	}

	starts, err := recurrence.ExpandOccurrences(master.Rrule, master.Tzid, master.Start, occurrenceStart, occurrenceStart.Add(time.Second))
	if err != nil {
		return false, fmt.Errorf("expand occurrences: %w", err)
	}
	for _, start := range starts {
		if start.Equal(occurrenceStart) {
			return true, nil
		}
	}
	return false, nil
}

// ListSeriesByCalendar returns every Master in calendarID (with Exdates and
// Reminders attached) alongside a parentID-keyed map of each Master's
// Overrides (each with its own Reminders attached) — CalDAV's per-calendar
// object listing (ADR-0025).
func (s *EventService) ListSeriesByCalendar(ctx context.Context, userID int64, calendarID string) ([]repository.Event, map[string][]repository.Event, error) {
	_, err := s.calendarByID(ctx, userID, calendarID)
	if err != nil {
		if errors.Is(err, ErrCalendarNotFound) {
			return []repository.Event{}, map[string][]repository.Event{}, nil
		}
		return nil, nil, err
	}

	masters, err := s.events.ListMastersByCalendar(ctx, calendarID)
	if err != nil {
		return nil, nil, fmt.Errorf("list masters: %w", err)
	}
	if err := s.attachExdates(ctx, masters); err != nil {
		return nil, nil, err
	}

	overridesByParent, err := s.attachOverridesAndReminders(ctx, masters, userID)
	if err != nil {
		return nil, nil, err
	}
	if err := s.attachAttachments(ctx, masters); err != nil {
		return nil, nil, err
	}
	return masters, overridesByParent, nil
}

// ListAttendeeOnlySeries returns, shaped like ListSeriesByCalendar, every
// series userID is an Attendee of (on its Master or any of its Overrides)
// whose Calendar userID has no Access to (ADR-0046, #163) — the content of
// the CalDAV backend's synthetic Attendee-only collection, which no real
// per-principal Calendar collection backs for userID. A series userID both
// has Calendar Access to and is an Attendee of is excluded here: it already
// appears once, correctly attributed, under that Calendar's own collection
// (ListSeriesByCalendar), and must not be listed twice.
func (s *EventService) ListAttendeeOnlySeries(ctx context.Context, userID int64) ([]repository.Event, map[string][]repository.Event, error) {
	accessible, err := s.calendars.ListAccessible(ctx, userID)
	if err != nil {
		return nil, nil, fmt.Errorf("list calendars: %w", err)
	}
	accessibleIDs := make(map[string]bool, len(accessible))
	for _, c := range accessible {
		accessibleIDs[c.ID] = true
	}

	attendeeEvents, err := s.events.ListByAttendeeUserID(ctx, userID, nil, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("list attendee events: %w", err)
	}

	masterIDs := []string{}
	seenMasterID := map[string]bool{}
	for _, e := range attendeeEvents {
		if accessibleIDs[e.CalendarID] {
			continue
		}
		masterID := e.ID
		if e.ParentID != nil {
			masterID = *e.ParentID
		}
		if seenMasterID[masterID] {
			continue
		}
		seenMasterID[masterID] = true
		masterIDs = append(masterIDs, masterID)
	}

	masters := make([]repository.Event, 0, len(masterIDs))
	for _, id := range masterIDs {
		master, err := s.events.GetByID(ctx, id)
		if err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				continue
			}
			return nil, nil, fmt.Errorf("get master event: %w", err)
		}
		masters = append(masters, master)
	}

	if err := s.attachExdates(ctx, masters); err != nil {
		return nil, nil, err
	}
	overridesByParent, err := s.attachOverridesAndReminders(ctx, masters, userID)
	if err != nil {
		return nil, nil, err
	}
	if err := s.attachAttachments(ctx, masters); err != nil {
		return nil, nil, err
	}
	return masters, overridesByParent, nil
}

// isAttendeeOfEventIDs reports whether userID is an Attendee of any of ids —
// GetAttendeeOnlySeries' visibility check, run across a Master and its
// Overrides since an Attendee invite can name either (#161).
func (s *EventService) isAttendeeOfEventIDs(ctx context.Context, userID int64, ids []string) (bool, error) {
	for _, id := range ids {
		if _, err := s.attendees.Get(ctx, id, userID); err == nil {
			return true, nil
		} else if !errors.Is(err, repository.ErrNotFound) {
			return false, err
		}
	}
	return false, nil
}

// GetAttendeeOnlySeries returns masterID's series (Master plus Overrides)
// for the CalDAV backend's synthetic Attendee-only collection (ADR-0046,
// #163): it resolves masterID whenever userID is an Attendee of it or of
// any of its Overrides and has no Calendar Access to it — the counterpart
// to GetSeries, which resolves purely through Calendar Access. A series
// userID both has Calendar Access to and is an Attendee of is excluded
// here, mirroring ListAttendeeOnlySeries: it resolves only through
// GetSeries, under its Calendar's own collection, never through this one.
// Returns repository.ErrNotFound if masterID doesn't exist, names an
// Override rather than a Master, userID is not an Attendee of it, or
// userID already has Calendar Access to it.
func (s *EventService) GetAttendeeOnlySeries(ctx context.Context, userID int64, masterID string) (repository.Event, []repository.Event, error) {
	master, err := s.events.GetByID(ctx, masterID)
	if err != nil {
		return repository.Event{}, nil, err
	}
	if master.ParentID != nil {
		return repository.Event{}, nil, repository.ErrNotFound
	}

	if _, err := s.calendarByID(ctx, userID, master.CalendarID); err == nil {
		return repository.Event{}, nil, repository.ErrNotFound
	} else if !errors.Is(err, ErrCalendarNotFound) {
		return repository.Event{}, nil, err
	}

	overridesByParent, err := s.events.ListChildrenByParentIDs(ctx, []string{masterID})
	if err != nil {
		return repository.Event{}, nil, fmt.Errorf("list overrides: %w", err)
	}
	overrides := overridesByParent[masterID]

	isAttendee, err := s.isAttendeeOfEventIDs(ctx, userID, append([]string{masterID}, eventIDs(overrides)...))
	if err != nil {
		return repository.Event{}, nil, err
	}
	if !isAttendee {
		return repository.Event{}, nil, repository.ErrNotFound
	}

	return s.hydrateSeries(ctx, userID, master, overrides)
}

// attachOverridesAndReminders loads each of masters' Overrides and attaches
// Reminders, Attendees, and each row's own Organizer to both masters and
// their Overrides in place, returning a parentID-keyed map of the
// Overrides. Shared by every read path that recomposes whole series —
// ListSeriesByCalendar, ListAttendeeOnlySeries and SyncSince (ADR-0025,
// ADR-0062). viewerID is whoever is asking, so Reminders resolve to their
// own rows rather than the Calendar's Owner's (ADR-0064).
func (s *EventService) attachOverridesAndReminders(ctx context.Context, masters []repository.Event, viewerID int64) (map[string][]repository.Event, error) {
	masterIDs := eventIDs(masters)
	overridesByParent, err := s.events.ListChildrenByParentIDs(ctx, masterIDs)
	if err != nil {
		return nil, fmt.Errorf("list overrides: %w", err)
	}

	// One batched Reminder lookup covers the Masters and every Override, by
	// pointing at each row where it already lives rather than copying them
	// into a flat slice and splitting the result back out afterwards.
	all := make([]*repository.Event, 0, len(masters))
	for i := range masters {
		all = append(all, &masters[i])
	}
	for _, id := range masterIDs {
		overrides := overridesByParent[id]
		for i := range overrides {
			all = append(all, &overrides[i])
		}
	}
	if err := s.attachViewerRemindersTo(ctx, all, viewerID); err != nil {
		return nil, err
	}
	if err := s.attachOrganizersAndAttendeesTo(ctx, all); err != nil {
		return nil, err
	}

	return overridesByParent, nil
}

// SyncResult is a sync-collection REPORT's diff: series changed (created or
// updated) since the caller's sync-token, series deleted since then, and the
// new high-water mark to hand back as the next sync-token (ADR-0025).
type SyncResult struct {
	Masters           []repository.Event
	OverridesByParent map[string][]repository.Event
	DeletedUIDs       []string
	NewToken          int64
}

// SyncSince computes calendarID's diff since sinceToken: every Master whose
// series changed (change_seq > sinceToken), every series deleted since then,
// and the calendar's current CTag as the new sync-token. sinceToken of 0
// returns every live series, matching an initial sync (ADR-0025, #65).
func (s *EventService) SyncSince(ctx context.Context, userID int64, calendarID string, sinceToken int64) (SyncResult, error) {
	_, err := s.calendarByID(ctx, userID, calendarID)
	if err != nil {
		if errors.Is(err, ErrCalendarNotFound) {
			return SyncResult{}, nil
		}
		return SyncResult{}, err
	}

	masters, err := s.events.ListMastersChangedSince(ctx, calendarID, sinceToken)
	if err != nil {
		return SyncResult{}, fmt.Errorf("list changed masters: %w", err)
	}
	if err := s.attachExdates(ctx, masters); err != nil {
		return SyncResult{}, err
	}

	overridesByParent, err := s.attachOverridesAndReminders(ctx, masters, userID)
	if err != nil {
		return SyncResult{}, err
	}

	deleted, err := s.sync.DeletedSince(ctx, calendarID, sinceToken)
	if err != nil {
		return SyncResult{}, fmt.Errorf("list deleted objects: %w", err)
	}
	deletedUIDs := make([]string, len(deleted))
	for i, d := range deleted {
		deletedUIDs[i] = d.UID
	}

	newToken, err := s.sync.CTag(ctx, calendarID)
	if err != nil {
		return SyncResult{}, fmt.Errorf("compute new sync-token: %w", err)
	}

	return SyncResult{
		Masters:           masters,
		OverridesByParent: overridesByParent,
		DeletedUIDs:       deletedUIDs,
		NewToken:          newToken,
	}, nil
}

// TouchChangeSeq bumps masterID's change_seq without touching any of its own
// columns, so a write to something outside the events table — an Attachment
// (#132/#133, ADR-0040) — still changes the Master's ETag (SeriesToICal picks
// up the new Attachment on the next read) and its Calendar's CTag. The caller
// has already resolved masterID and checked Access; this trusts that.
func (s *EventService) TouchChangeSeq(ctx context.Context, masterID string) error {
	return s.withTx(ctx, func(repos txRepos) error {
		seq, err := repos.sync.NextChangeSeq(ctx)
		if err != nil {
			return fmt.Errorf("next change seq: %w", err)
		}
		if err := repos.events.SetChangeSeq(ctx, masterID, seq); err != nil {
			return fmt.Errorf("bump change_seq: %w", err)
		}
		return nil
	})
}

// CalendarCTag returns calendarID's CTag — the highest change_seq among its
// live series and its tombstones — for CalDAV's getctag property
// (ADR-0025). Unlike every other EventService method, this never took a
// userID before ADR-0034 — calendarID was an unguessable UUID belonging to
// the only user, so nothing else checked it either. Access now applies here
// too.
func (s *EventService) CalendarCTag(ctx context.Context, userID int64, calendarID string) (int64, error) {
	if _, err := s.calendarByID(ctx, userID, calendarID); err != nil {
		return 0, err
	}
	return s.sync.CTag(ctx, calendarID)
}

// AttendeeOnlyCTag is CalendarCTag's counterpart for the CalDAV backend's
// synthetic Attendee-only collection (#163): no single repository.Calendar
// row (and so no single change_seq/tombstone stream, unlike sync.CTag)
// backs a view spanning every Calendar userID isn't otherwise Accessing, so
// this folds every current member series' own ChangeSeq together with how
// many series currently qualify — an edit to an existing series changes the
// former, an Attendee invite being added, revoked, or newly excluded by
// gaining Calendar Access changes the latter, and either changes the CTag.
func (s *EventService) AttendeeOnlyCTag(ctx context.Context, userID int64) (int64, error) {
	masters, _, err := s.ListAttendeeOnlySeries(ctx, userID)
	if err != nil {
		return 0, err
	}

	ctag := int64(len(masters))
	for _, master := range masters {
		ctag += master.ChangeSeq
	}
	return ctag, nil
}

// Delete removes id's row. Deleting a Master removes a whole series, which
// leaves no row for sync-collection to diff against, so its removal is
// recorded as a tombstone instead; deleting an Override still leaves its
// Master's calendar object changed, so the Master's change_seq bumps
// instead (ADR-0025). Withdraws every affected row's own Invitations with a
// METHOD:CANCEL (ADR-0059, #201): deleting a Master cancels its own
// Attendees and, separately, each of its Overrides' own Attendees with that
// row's own RECURRENCE-ID — a series is never treated as a single Event for
// this — captured before anything is removed, since a CANCEL must outlive
// the row it withdraws.
func (s *EventService) Delete(ctx context.Context, userID int64, id string) error {
	existing, err := s.getOwnedEvent(ctx, userID, id)
	if err != nil {
		return err
	}
	if err := s.requireWritableCalendar(ctx, userID, existing.CalendarID); err != nil {
		return err
	}

	return s.withTx(ctx, func(repos txRepos) error {
		rows := []repository.Event{existing}
		if existing.ParentID == nil {
			children, err := repos.events.ListChildrenByParentIDs(ctx, []string{id})
			if err != nil {
				return fmt.Errorf("list children for cancellation: %w", err)
			}
			rows = append(rows, children[id]...)
		}
		for _, row := range rows {
			if err := s.enqueueCancellationsForRow(ctx, repos, row); err != nil {
				return err
			}
		}

		if err := repos.events.Delete(ctx, id); err != nil {
			return err
		}

		seq, err := repos.sync.NextChangeSeq(ctx)
		if err != nil {
			return err
		}

		if existing.ParentID == nil {
			return repos.sync.Tombstone(ctx, existing.CalendarID, id, seq)
		}
		if err := repos.events.SetChangeSeq(ctx, *existing.ParentID, seq); err != nil {
			return fmt.Errorf("bump parent change_seq: %w", err)
		}
		return nil
	})
}

// AddException cancels a single Occurrence of a recurring Master (deleting
// "this event"): the rule still generates that slot, but it is suppressed
// from expansion (ADR-0016).
func (s *EventService) AddException(ctx context.Context, userID int64, parentID string, occurrenceStart time.Time) error {
	parent, err := s.getOwnedEvent(ctx, userID, parentID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return ErrParentNotFound
		}
		return err
	}
	if parent.ParentID != nil {
		return ErrParentIsOverride
	}
	if parent.Rrule == "" {
		return ErrParentNotRecurring
	}
	if err := s.requireWritableCalendar(ctx, userID, parent.CalendarID); err != nil {
		return err
	}

	return s.withTx(ctx, func(repos txRepos) error {
		if err := repos.exceptions.Add(ctx, parentID, occurrenceStart); err != nil {
			return fmt.Errorf("add exception: %w", err)
		}
		seq, err := repos.sync.NextChangeSeq(ctx)
		if err != nil {
			return err
		}
		if err := repos.events.SetChangeSeq(ctx, parentID, seq); err != nil {
			return fmt.Errorf("bump parent change_seq: %w", err)
		}
		return nil
	})
}

// ReparentFrom moves every Override/Exception of oldParentID at-or-after
// fromStart to belong to newParentID instead — the "this and following" split
// reparenting at the boundary (ADR-0016). Both events must already exist and
// belong to the caller. A moved Override's own Attendees, if any, are
// withdrawn under its old UID and re-invited under its new one (ADR-0059,
// #201) — see the withTx body's own comment.
func (s *EventService) ReparentFrom(ctx context.Context, userID int64, oldParentID, newParentID string, fromStart time.Time) error {
	oldParent, err := s.getOwnedEvent(ctx, userID, oldParentID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return ErrParentNotFound
		}
		return err
	}
	newParent, err := s.getOwnedEvent(ctx, userID, newParentID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return ErrParentNotFound
		}
		return err
	}

	if err := s.requireWritableCalendar(ctx, userID, oldParent.CalendarID); err != nil {
		return err
	}
	if newParent.CalendarID != oldParent.CalendarID {
		if err := s.requireWritableCalendar(ctx, userID, newParent.CalendarID); err != nil {
			return err
		}
	}

	return s.withTx(ctx, func(repos txRepos) error {
		// Reparenting changes a moved Override's own iTIP UID — Invitations
		// key it off ParentID (icalendar.InvitationToICal) — which a
		// recipient's client can only ever be told about via cancel-old,
		// invite-new; there is no in-place SEQUENCE bump for a UID change.
		// So each moved Override's own Attendees, if any, are told now:
		// captured, and cancelled under oldParentID, before
		// ReparentOverridesFrom below moves the row and a fresh REQUEST —
		// scheduled here but, like every REQUEST, rendered from live state
		// only once it actually sends — picks up newParentID instead
		// (ADR-0059, #201).
		childrenByOldParent, err := repos.events.ListChildrenByParentIDs(ctx, []string{oldParentID})
		if err != nil {
			return fmt.Errorf("list children to reparent: %w", err)
		}
		for _, child := range childrenByOldParent[oldParentID] {
			if child.RecurrenceID == nil || child.RecurrenceID.Before(fromStart) {
				continue
			}
			if err := s.enqueueCancellationsForRow(ctx, repos, child); err != nil {
				return err
			}
			if err := s.enqueueReinvitations(ctx, repos, child.ID, nil, nil); err != nil {
				return err
			}
		}

		if err := repos.events.ReparentOverridesFrom(ctx, oldParentID, newParentID, fromStart); err != nil {
			return fmt.Errorf("reparent overrides: %w", err)
		}
		if err := repos.exceptions.ReparentFrom(ctx, oldParentID, newParentID, fromStart); err != nil {
			return fmt.Errorf("reparent exceptions: %w", err)
		}

		// The split changes both series' calendar objects — occurrences move
		// from one to the other — so both Masters' change_seq bump
		// (ADR-0025).
		seq, err := repos.sync.NextChangeSeq(ctx)
		if err != nil {
			return err
		}
		if err := repos.events.SetChangeSeq(ctx, oldParentID, seq); err != nil {
			return fmt.Errorf("bump old parent change_seq: %w", err)
		}
		if err := repos.events.SetChangeSeq(ctx, newParentID, seq); err != nil {
			return fmt.Errorf("bump new parent change_seq: %w", err)
		}
		return nil
	})
}

// SeriesWrite is a whole series' Master fields plus its Overrides and
// Exdates, decomposed from an incoming CalDAV PUT (ADR-0025).
type SeriesWrite struct {
	Title, Description, Location, URL string
	Start, End                        time.Time
	AllDay                            bool
	Tzid                              *string
	Rrule                             string
	Reminders                         []repository.Reminder
	Exdates                           []time.Time
	Overrides                         []OverrideWrite
	// Color mirrors EventWrite.Color — the Master's own color override, nil
	// meaning "inherit the Calendar's color" (ADR-0043).
	Color *string
	// ExternalUID is set only when ImportSeries is writing a Subscribed
	// Calendar's Events (#83, ADR-0033) — empty for ordinary import and
	// CalDAV PUT, which leave the row's external_uid column NULL.
	ExternalUID string
	// Attachments are Attachments already saved to disk (ADR-0040's
	// before-commit ordering) for writeSeries to row alongside the Master —
	// populated only by ordinary ICS import (#135); every other SeriesWrite
	// source (CalDAV PUT, Subscribe/Refresh) leaves this empty, since
	// Attachments arrive only through Import's own POST actions or ICS
	// import, never through a PUT or an unattended poll.
	Attachments []AttachmentWrite
}

// AttachmentWrite is one Attachment whose bytes are already on disk under
// ID, ready for writeSeries to create its row inside the same transaction as
// its series' Master — ICS import's counterpart to AttachmentService.Upload,
// which does the same two steps (save, then row) outside a series write
// (#135, ADR-0040).
type AttachmentWrite struct {
	ID, Filename, ContentType string
	SizeBytes                 int64
}

// OverrideWrite is one Override VEVENT's fields, keyed by the Occurrence it
// replaces (RecurrenceID).
type OverrideWrite struct {
	RecurrenceID                      time.Time
	Title, Description, Location, URL string
	Start, End                        time.Time
	AllDay                            bool
	Tzid                              *string
	Reminders                         []repository.Reminder
	// ExternalUID mirrors SeriesWrite.ExternalUID — an Override shares its
	// Master's foreign UID (#83, ADR-0033).
	ExternalUID string
	// Color mirrors SeriesWrite.Color, scoped to this Override alone
	// (ADR-0043).
	Color *string
}

// validateEventFields applies the Create/Update validation shared by every
// VEVENT PutSeries writes (Master or Override), returning the trimmed title.
func validateEventFields(title string, start, end time.Time, reminders []repository.Reminder) (string, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return "", ErrInvalidTitle
	}
	if !end.After(start) {
		return "", ErrInvalidTimeRange
	}
	if err := validateReminders(reminders); err != nil {
		return "", err
	}
	return title, nil
}

// validateSeriesWrites applies validateEventFields (trimming each title in
// place) and the recurrence-rule check to every write and its Overrides,
// stopping at the first failure without writing anything. Shared by
// ImportSeries and ImportService.Import, which both need the same check to
// run before any Calendar is created or any row written — including on a
// dry run, so a preview reports exactly the errors a real run would hit
// (ADR-0030).
func validateSeriesWrites(writes []SeriesWrite) error {
	for i, w := range writes {
		title, err := validateEventFields(w.Title, w.Start, w.End, w.Reminders)
		if err != nil {
			return err
		}
		writes[i].Title = title
		if !isValidRecurrenceRule(w.Rrule) {
			return ErrInvalidRecurrenceRule
		}
		color, err := normalizeEventColor(w.Color)
		if err != nil {
			return err
		}
		writes[i].Color = color
		for j, o := range w.Overrides {
			trimmed, err := validateEventFields(o.Title, o.Start, o.End, o.Reminders)
			if err != nil {
				return err
			}
			writes[i].Overrides[j].Title = trimmed
			overrideColor, err := normalizeEventColor(o.Color)
			if err != nil {
				return err
			}
			writes[i].Overrides[j].Color = overrideColor
		}
	}
	return nil
}

// PutSeries atomically decomposes a CalDAV PUT into masterID's Master row,
// its Overrides, and its Exdates (ADR-0025): masterID is created if it
// doesn't already exist yet, updated in place if it does; each Override in
// write.Overrides is matched to an existing row by RecurrenceID — updated if
// found, created if not — and any existing Override absent from
// write.Overrides is deleted (the device removed it); Exdates are replaced
// wholesale. The whole write bumps change_seq exactly once, so CTag and
// sync-collection see one atomic change (ADR-0018). Returns the written
// series recomposed exactly as GetSeries would.
func (s *EventService) PutSeries(ctx context.Context, userID int64, calendarID, masterID string, write SeriesWrite) (repository.Event, []repository.Event, error) {
	title, err := validateEventFields(write.Title, write.Start, write.End, write.Reminders)
	if err != nil {
		return repository.Event{}, nil, err
	}
	if !isValidRecurrenceRule(write.Rrule) {
		return repository.Event{}, nil, ErrInvalidRecurrenceRule
	}
	color, err := normalizeEventColor(write.Color)
	if err != nil {
		return repository.Event{}, nil, err
	}
	write.Color = color
	for i, o := range write.Overrides {
		trimmed, err := validateEventFields(o.Title, o.Start, o.End, o.Reminders)
		if err != nil {
			return repository.Event{}, nil, err
		}
		write.Overrides[i].Title = trimmed
		overrideColor, err := normalizeEventColor(o.Color)
		if err != nil {
			return repository.Event{}, nil, err
		}
		write.Overrides[i].Color = overrideColor
	}

	if err := s.requireWritableCalendar(ctx, userID, calendarID); err != nil {
		return repository.Event{}, nil, err
	}

	existingMaster, err := s.getOwnedEvent(ctx, userID, masterID)
	masterExists := err == nil
	if err != nil && !errors.Is(err, repository.ErrNotFound) {
		return repository.Event{}, nil, err
	}
	if masterExists && existingMaster.ParentID != nil {
		return repository.Event{}, nil, ErrParentIsOverride
	}
	if masterExists && existingMaster.CalendarID != calendarID {
		return repository.Event{}, nil, repository.ErrNotFound
	}

	var existingOverrides []repository.Event
	if masterExists {
		overridesByParent, err := s.events.ListChildrenByParentIDs(ctx, []string{masterID})
		if err != nil {
			return repository.Event{}, nil, fmt.Errorf("list existing overrides: %w", err)
		}
		existingOverrides = overridesByParent[masterID]
	}
	existingByRecurrenceID := make(map[int64]repository.Event, len(existingOverrides))
	for _, o := range existingOverrides {
		existingByRecurrenceID[o.RecurrenceID.UnixNano()] = o
	}

	err = s.withTx(ctx, func(repos txRepos) error {
		seq, err := repos.sync.NextChangeSeq(ctx)
		if err != nil {
			return err
		}

		master := repository.EventFields{
			CalendarID:  calendarID,
			Title:       title,
			Start:       write.Start,
			End:         write.End,
			AllDay:      write.AllDay,
			Rrule:       write.Rrule,
			Tzid:        write.Tzid,
			Description: write.Description,
			Location:    write.Location,
			URL:         write.URL,
			Color:       write.Color,
		}
		if masterExists {
			// A CalDAV PUT carries no iTIP lifecycle of its own (ADR-0062:
			// CalDAV emits ATTENDEE but ignores it inbound) — sequence is
			// simply preserved rather than material-change detected.
			if _, err := repos.events.Update(ctx, masterID, master, seq, existingMaster.Sequence); err != nil {
				return fmt.Errorf("update master: %w", err)
			}
		} else {
			if _, err := repos.events.Create(ctx, masterID, &userID, master, seq); err != nil {
				return fmt.Errorf("create master: %w", err)
			}
		}
		// A PUT's VALARMs write the PUTting User's Reminders, not the
		// Calendar's Owner's (ADR-0064).
		if err := repos.reminders.ReplaceByEventID(ctx, userID, masterID, write.Reminders); err != nil {
			return fmt.Errorf("persist master reminders: %w", err)
		}

		if err := repos.exceptions.DeleteByParentID(ctx, masterID); err != nil {
			return fmt.Errorf("clear exdates: %w", err)
		}
		for _, exdate := range write.Exdates {
			if err := repos.exceptions.Add(ctx, masterID, exdate); err != nil {
				return fmt.Errorf("add exdate: %w", err)
			}
		}

		seen := make(map[int64]bool, len(write.Overrides))
		for _, o := range write.Overrides {
			o := o
			key := o.RecurrenceID.UnixNano()
			seen[key] = true

			// An Override never carries a rule of its own (ADR-0016), hence the
			// zero Rrule.
			override := repository.EventFields{
				CalendarID:  calendarID,
				Title:       o.Title,
				Start:       o.Start,
				End:         o.End,
				AllDay:      o.AllDay,
				Tzid:        o.Tzid,
				Description: o.Description,
				Location:    o.Location,
				URL:         o.URL,
				Color:       o.Color,
			}

			if existing, ok := existingByRecurrenceID[key]; ok {
				if _, err := repos.events.Update(ctx, existing.ID, override, seq, existing.Sequence); err != nil {
					return fmt.Errorf("update override: %w", err)
				}
				if err := repos.reminders.ReplaceByEventID(ctx, userID, existing.ID, o.Reminders); err != nil {
					return fmt.Errorf("persist override reminders: %w", err)
				}
				continue
			}

			override.ParentID = &masterID
			override.RecurrenceID = &o.RecurrenceID

			overrideID := uuid.NewString()
			if _, err := repos.events.Create(ctx, overrideID, &userID, override, seq); err != nil {
				return fmt.Errorf("create override: %w", err)
			}
			if err := repos.reminders.ReplaceByEventID(ctx, userID, overrideID, o.Reminders); err != nil {
				return fmt.Errorf("persist override reminders: %w", err)
			}
		}

		for key, existing := range existingByRecurrenceID {
			if seen[key] {
				continue
			}
			if err := repos.events.Delete(ctx, existing.ID); err != nil {
				return fmt.Errorf("delete removed override: %w", err)
			}
		}

		return nil
	})
	if err != nil {
		return repository.Event{}, nil, fmt.Errorf("put series: %w", err)
	}

	return s.GetSeries(ctx, userID, masterID)
}

// attendeeManagementCalendar resolves eventID and confirms actorUserID holds
// Editor Access (or is the Owner) of its Calendar — AddAttendee and
// RemoveAttendee's shared guard, since Attendee management is a Calendar
// write like any other Event edit (#161), not something an Attendee with no
// Calendar Access could ever do to their own invite. Also returns the
// Calendar, so callers can resolve its WorkspaceID without a second lookup.
func (s *EventService) attendeeManagementCalendar(ctx context.Context, actorUserID int64, eventID string) (repository.Event, repository.Calendar, error) {
	event, err := s.events.GetByID(ctx, eventID)
	if err != nil {
		return repository.Event{}, repository.Calendar{}, err
	}
	if err := s.requireWritableCalendar(ctx, actorUserID, event.CalendarID); err != nil {
		if errors.Is(err, ErrCalendarNotFound) {
			return repository.Event{}, repository.Calendar{}, repository.ErrNotFound
		}
		return repository.Event{}, repository.Calendar{}, err
	}
	calendar, err := s.calendarByID(ctx, actorUserID, event.CalendarID)
	if err != nil {
		return repository.Event{}, repository.Calendar{}, err
	}
	return event, calendar, nil
}

// enqueueReinvitations re-queues a REQUEST Invitation to every one of
// eventID's own current Attendees except those named in exceptUserIDs/
// exceptEmails — Update's ADR-0059 re-send, run whenever contentChanged said
// something an Invitation renders actually changed, and AddAttendee/
// AddGroupAttendee/RemoveAttendee's own re-send for everyone *else*: the
// Attendee list itself is a non-material change that still re-sends (every
// REQUEST renders the row's whole current ATTENDEE list, ADR-0062), but the
// one Attendee just added or removed already gets their own dedicated
// message — a fresh invite or a CANCEL — so they're excluded here to avoid
// sending them a redundant second one. Both maps are nil from Update's own
// call, which excludes nobody. Reuses OutboxRepository.Enqueue/EnqueueEmail,
// exactly like inviteUser/inviteEmail do for a brand-new Attendee: the
// message carries no snapshot of its own (repository.OutboxMessage's
// Method-REQUEST half), since InvitationSender always rebuilds a REQUEST
// from live state — including the row's own Sequence, already bumped (or
// not) by the caller's write before this runs. A no-op when outbox is nil
// (no SMTP configured) or eventID has no other Attendees.
func (s *EventService) enqueueReinvitations(ctx context.Context, repos txRepos, eventID string, exceptUserIDs map[int64]bool, exceptEmails map[string]bool) error {
	if repos.outbox == nil {
		return nil
	}
	attendees, err := repos.attendees.ListByEventID(ctx, eventID)
	if err != nil {
		return fmt.Errorf("list attendees for re-invitation: %w", err)
	}
	for _, a := range attendees {
		if a.UserID != nil {
			if exceptUserIDs[*a.UserID] {
				continue
			}
			if _, err := repos.outbox.Enqueue(ctx, eventID, *a.UserID); err != nil {
				return fmt.Errorf("enqueue re-invitation: %w", err)
			}
			continue
		}
		if exceptEmails[a.Email] {
			continue
		}
		if _, err := repos.outbox.EnqueueEmail(ctx, eventID, a.Email); err != nil {
			return fmt.Errorf("enqueue re-invitation: %w", err)
		}
	}
	return nil
}

// enqueueCancellation queues one CANCEL withdrawing row's Invitation from a
// single Attendee — exactly one of userID/email is set, mirroring
// inviteUser/inviteEmail's own split (ADR-0058). uid is row's iTIP UID: its
// own id on a Master or a standalone Event, its Master's id on an Override
// (icalendar.InvitationToICal's contract). Captures row's current fields
// plus its own Organizer into repository.OutboxCancelSnapshot right now,
// rather than a live lookup at send time — a CANCEL's purpose is to outlive
// the row (or the Attendee row) it withdraws, unlike a REQUEST.
func (s *EventService) enqueueCancellation(ctx context.Context, repos txRepos, row repository.Event, recipientName, recipientEmail string, userID *int64, email *string) error {
	uid := row.ID
	if row.ParentID != nil {
		uid = *row.ParentID
	}

	var organizerName, organizerEmail string
	if row.CreatedBy != nil {
		organizer, err := repos.users.GetByID(ctx, *row.CreatedBy)
		if err != nil && !errors.Is(err, repository.ErrNotFound) {
			return fmt.Errorf("look up organizer: %w", err)
		}
		organizerName, organizerEmail = organizer.Name, organizer.Email
	}

	snapshot := repository.OutboxCancelSnapshot{
		UID:            uid,
		RecurrenceID:   row.RecurrenceID,
		AllDay:         row.AllDay,
		Tzid:           row.Tzid,
		Start:          row.Start,
		End:            row.End,
		Title:          row.Title,
		Sequence:       row.Sequence,
		OrganizerName:  organizerName,
		OrganizerEmail: organizerEmail,
		RecipientEmail: recipientEmail,
		RecipientName:  recipientName,
	}

	if userID != nil {
		if _, err := repos.outbox.EnqueueCancel(ctx, row.ID, *userID, snapshot); err != nil {
			return fmt.Errorf("enqueue cancellation: %w", err)
		}
		return nil
	}
	if _, err := repos.outbox.EnqueueCancelEmail(ctx, row.ID, *email, snapshot); err != nil {
		return fmt.Errorf("enqueue cancellation: %w", err)
	}
	return nil
}

// enqueueCancellationsForRow queues a CANCEL to every one of row's own
// current Attendees — Delete's per-row fan-out over enqueueCancellation, one
// per Attendee. A no-op when outbox is nil.
func (s *EventService) enqueueCancellationsForRow(ctx context.Context, repos txRepos, row repository.Event) error {
	if repos.outbox == nil {
		return nil
	}
	attendees, err := repos.attendees.ListByEventID(ctx, row.ID)
	if err != nil {
		return fmt.Errorf("list attendees for cancellation: %w", err)
	}
	for _, a := range attendees {
		if a.UserID != nil {
			if err := s.enqueueCancellation(ctx, repos, row, a.Name, a.Email, a.UserID, nil); err != nil {
				return err
			}
			continue
		}
		email := a.Email
		if err := s.enqueueCancellation(ctx, repos, row, a.Name, a.Email, nil, &email); err != nil {
			return err
		}
	}
	return nil
}

// inviteUser is AddAttendee's and Create's shared explicit-target check
// (#187, ADR-0055): targetUserID must exist, not be Disabled (hidden from
// the invite picker, ADR-0037 — from the inviter's perspective they don't
// exist to invite, same as CalendarService.Share), and already be a Member
// of workspaceID, before their attendees row is inserted. Takes the User,
// Workspace, Attendee, and Notification repositories directly rather than
// through EventService or txRepos, so the identical check can run against
// either the pooled repos (AddAttendee, outside any transaction) or the
// transaction-bound ones (Create, inside its own) without duplicating the
// checks themselves.
//
// Writing an invite Notification for targetUserID here, right alongside the
// attendees row, is what makes "an invite Notification is written when an
// Attendee row naming a User is written" (ADR-0061) true regardless of
// which of inviteUser's three callers (AddAttendee, addCreateAttendees,
// expandGroupMembers's sibling) is the one doing the writing. Enqueuing an
// Invitation alongside it, when outbox is non-nil, does the same for
// ADR-0059/ADR-0060: the outbox row commits in the same transaction as the
// attendees row it belongs to, so a failed send never loses the Attendee
// and a rolled-back invite queues nothing. outbox is nil when this
// deployment has no SMTP transport configured (EventService.outbox), in
// which case nothing is queued and the invite still succeeds — and nothing
// is charged against actorUserID's rate limit either (#204, ADR-0058),
// since chargeInviteRateLimit only runs alongside an actual enqueue.
func inviteUser(ctx context.Context, users *repository.UserRepository, workspaces *repository.WorkspaceRepository, attendees *repository.AttendeeRepository, notifications *repository.NotificationRepository, outbox *repository.OutboxRepository, event repository.Event, workspaceID, targetUserID, actorUserID int64, inviteRateLimitPerHour int) (repository.Attendee, error) {
	target, err := users.GetByID(ctx, targetUserID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return repository.Attendee{}, ErrUserNotFound
		}
		return repository.Attendee{}, fmt.Errorf("look up user: %w", err)
	}
	if target.IsDisabled {
		return repository.Attendee{}, ErrUserNotFound
	}

	if _, err := workspaces.GetMember(ctx, workspaceID, targetUserID); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return repository.Attendee{}, ErrAttendeeTargetNotInWorkspace
		}
		return repository.Attendee{}, fmt.Errorf("get workspace member: %w", err)
	}

	attendee, err := attendees.Add(ctx, event.ID, targetUserID)
	if err != nil {
		return repository.Attendee{}, err
	}
	if _, err := notifications.InsertInvite(ctx, targetUserID, event.ID, event.Title, time.Now()); err != nil {
		return repository.Attendee{}, fmt.Errorf("insert invite notification: %w", err)
	}
	if outbox != nil {
		if err := chargeInviteRateLimit(ctx, outbox, actorUserID, inviteRateLimitPerHour); err != nil {
			return repository.Attendee{}, err
		}
		if _, err := outbox.EnqueueWithActor(ctx, event.ID, targetUserID, actorUserID); err != nil {
			return repository.Attendee{}, fmt.Errorf("enqueue invitation: %w", err)
		}
	}
	return attendee, nil
}

// inviteEmail is AddAttendeeByEmail's and Create's shared typed-address
// check (ADR-0058, #200): rawEmail is validated and folded the same way
// every other login-identifier-shaped input in this app is
// (service.validateEmail), then resolved against workspaceID's Members
// before anything is written. A match writes a User-backed Attendee by
// delegating straight to inviteUser — which is also what makes a Disabled
// Member's address rejected rather than downgraded to an email row: it's
// the same disabled check inviteUser already runs for an explicit user id
// target, inherited for free rather than duplicated. Anything else
// (including the same address belonging to a User in a *different*
// Workspace on this instance — ADR-0058's deliberate "treated as an
// outsider") writes an email-shaped Attendee instead, with no Notification
// (no account to notify) and, when outbox is configured, its own Invitation
// enqueued.
//
// The Event's own Organizer's address is rejected outright, before the
// Member lookup even runs — admitting it is one SetResponse away from an
// Organizer who declined their own meeting (ADR-0055). Takes the User,
// Workspace, Attendee, and Notification repositories directly, same reason
// as inviteUser: the identical check must run against both the pooled repos
// (AddAttendeeByEmail) and the transaction-bound ones (Create). actorUserID
// and inviteRateLimitPerHour are inviteUser's own rate-limit params (#204,
// ADR-0058), threaded through to the inviteUser delegation below and
// applied directly to the email-shaped write.
func inviteEmail(ctx context.Context, users *repository.UserRepository, workspaces *repository.WorkspaceRepository, attendees *repository.AttendeeRepository, notifications *repository.NotificationRepository, outbox *repository.OutboxRepository, event repository.Event, workspaceID int64, rawEmail string, actorUserID int64, inviteRateLimitPerHour int) (repository.AttendeeWithName, error) {
	email, err := validateEmail(rawEmail)
	if err != nil {
		return repository.AttendeeWithName{}, ErrInvalidEmail
	}

	if event.CreatedBy != nil {
		organizer, err := users.GetByID(ctx, *event.CreatedBy)
		if err != nil && !errors.Is(err, repository.ErrNotFound) {
			return repository.AttendeeWithName{}, fmt.Errorf("look up organizer: %w", err)
		}
		if err == nil && organizer.Email == email {
			return repository.AttendeeWithName{}, ErrAttendeeIsOrganizer
		}
	}

	target, err := users.GetByEmail(ctx, email)
	if err != nil && !errors.Is(err, repository.ErrNotFound) {
		return repository.AttendeeWithName{}, fmt.Errorf("look up user by email: %w", err)
	}
	if err == nil {
		if _, memErr := workspaces.GetMember(ctx, workspaceID, target.ID); memErr == nil {
			attendee, err := inviteUser(ctx, users, workspaces, attendees, notifications, outbox, event, workspaceID, target.ID, actorUserID, inviteRateLimitPerHour)
			if err != nil {
				return repository.AttendeeWithName{}, err
			}
			userID := attendee.UserID
			return repository.AttendeeWithName{EventID: attendee.EventID, UserID: &userID, Response: attendee.Response, CreatedAt: attendee.CreatedAt, Name: target.Name, Email: target.Email}, nil
		} else if !errors.Is(memErr, repository.ErrNotFound) {
			return repository.AttendeeWithName{}, fmt.Errorf("get workspace member: %w", memErr)
		}
		// target exists on this instance but in a different Workspace (or
		// none at all) — an outsider from this Event's Workspace's point of
		// view, so falls through to the email-shaped write below (ADR-0058).
	}

	if outbox == nil {
		return repository.AttendeeWithName{}, ErrAttendeeEmailInvitesUnavailable
	}

	added, err := attendees.AddEmail(ctx, event.ID, email)
	if err != nil {
		return repository.AttendeeWithName{}, err
	}
	if err := chargeInviteRateLimit(ctx, outbox, actorUserID, inviteRateLimitPerHour); err != nil {
		return repository.AttendeeWithName{}, err
	}
	if _, err := outbox.EnqueueEmailWithActor(ctx, event.ID, email, actorUserID); err != nil {
		return repository.AttendeeWithName{}, fmt.Errorf("enqueue invitation: %w", err)
	}
	return added, nil
}

// expandGroupMembers is AddGroupAttendee's and Create's shared Group
// expansion (#162, #187, ADR-0046, ADR-0055): one attendees row per current,
// enabled member of groupID. A Disabled member is silently skipped, same as
// inviteUser refuses to invite one directly (ADR-0037) — so a Group invite
// can never produce an Attendee an individual invite would have refused. A
// member who is already an Attendee of eventID (invited individually, or via
// another Group) is left untouched rather than failing the whole expansion.
// Takes the Group, User, Attendee, and Notification repositories directly,
// same reason as inviteUser above — AddGroupAttendee always calls this with
// transaction-bound repos (reading membership and writing Attendees in the
// same transaction keeps the snapshot honest against a concurrent
// membership change); Create's own call is already inside its own
// transaction. Writes one invite Notification per Attendee row actually
// added, same as inviteUser (ADR-0061) — a member already an Attendee is
// skipped and gets no second Notification. Enqueues one Invitation per
// Attendee row actually added too, same as inviteUser, when outbox is
// non-nil (ADR-0059, ADR-0060) — each one charged against actorUserID's own
// hourly ceiling (#204, ADR-0058), same as an individual invite; a large
// Group hitting the ceiling partway through fails the whole expansion via
// the caller's transaction rollback rather than inviting some members and
// silently not others.
func expandGroupMembers(ctx context.Context, groups *repository.GroupRepository, users *repository.UserRepository, attendees *repository.AttendeeRepository, notifications *repository.NotificationRepository, outbox *repository.OutboxRepository, event repository.Event, groupID, actorUserID int64, inviteRateLimitPerHour int) ([]repository.Attendee, error) {
	members, err := groups.ListMembers(ctx, groupID)
	if err != nil {
		return nil, fmt.Errorf("list group members: %w", err)
	}

	added := []repository.Attendee{}
	for _, member := range members {
		target, err := users.GetByID(ctx, member.UserID)
		if err != nil {
			return nil, fmt.Errorf("look up group member: %w", err)
		}
		if target.IsDisabled {
			continue
		}

		attendee, err := attendees.Add(ctx, event.ID, member.UserID)
		if err != nil {
			if errors.Is(err, repository.ErrAlreadyAttendee) {
				continue
			}
			return nil, err
		}
		if _, err := notifications.InsertInvite(ctx, member.UserID, event.ID, event.Title, time.Now()); err != nil {
			return nil, fmt.Errorf("insert invite notification: %w", err)
		}
		if outbox != nil {
			if err := chargeInviteRateLimit(ctx, outbox, actorUserID, inviteRateLimitPerHour); err != nil {
				return nil, err
			}
			if _, err := outbox.EnqueueWithActor(ctx, event.ID, member.UserID, actorUserID); err != nil {
				return nil, fmt.Errorf("enqueue invitation: %w", err)
			}
		}
		added = append(added, attendee)
	}
	return added, nil
}

// AddAttendee invites targetUserID as an Attendee of eventID, callable by
// any caller with Editor Access to (or ownership of) eventID's Calendar
// (#161, ADR-0046). Being an Attendee grants targetUserID visibility to
// eventID alone, with no Calendar Access of their own required — the invite
// itself is the grant. targetUserID must already be a Member of eventID's
// Calendar's own Workspace and not Disabled — inviteUser's shared check.
// Also re-sends the Invitation to every one of eventID's other current
// Attendees, unbumped: the Attendee list is itself a non-material change
// (ADR-0059) — every REQUEST renders the row's whole current list
// (ADR-0062) — and targetUserID already gets their own fresh invite, so
// they're excluded from this re-send.
func (s *EventService) AddAttendee(ctx context.Context, actorUserID int64, eventID string, targetUserID int64) (repository.Attendee, error) {
	event, calendar, err := s.attendeeManagementCalendar(ctx, actorUserID, eventID)
	if err != nil {
		return repository.Attendee{}, err
	}

	var attendee repository.Attendee
	err = s.withTx(ctx, func(repos txRepos) error {
		var err error
		attendee, err = inviteUser(ctx, repos.users, repos.workspaces, repos.attendees, repos.notifications, repos.outbox, event, calendar.WorkspaceID, targetUserID, actorUserID, s.inviteRateLimitPerHour)
		if err != nil {
			return err
		}
		return s.enqueueReinvitations(ctx, repos, eventID, map[int64]bool{targetUserID: true}, nil)
	})
	if err != nil {
		return repository.Attendee{}, err
	}
	return attendee, nil
}

// AddAttendeeByEmail invites rawEmail as an Attendee of eventID (ADR-0058,
// #200), callable by any caller with Editor Access to (or ownership of)
// eventID's Calendar, same as AddAttendee. rawEmail is resolved against
// eventID's Calendar's own Workspace before anything is written —
// inviteEmail's shared check — producing a User-backed Attendee on a match
// and an email-shaped one otherwise. Re-sends to every other current
// Attendee, same as AddAttendee (ADR-0059, #201), excluding whichever shape
// the new Attendee actually took.
func (s *EventService) AddAttendeeByEmail(ctx context.Context, actorUserID int64, eventID string, rawEmail string) (repository.AttendeeWithName, error) {
	event, calendar, err := s.attendeeManagementCalendar(ctx, actorUserID, eventID)
	if err != nil {
		return repository.AttendeeWithName{}, err
	}

	var attendee repository.AttendeeWithName
	err = s.withTx(ctx, func(repos txRepos) error {
		var err error
		attendee, err = inviteEmail(ctx, repos.users, repos.workspaces, repos.attendees, repos.notifications, repos.outbox, event, calendar.WorkspaceID, rawEmail, actorUserID, s.inviteRateLimitPerHour)
		if err != nil {
			return err
		}
		if attendee.UserID != nil {
			return s.enqueueReinvitations(ctx, repos, eventID, map[int64]bool{*attendee.UserID: true}, nil)
		}
		return s.enqueueReinvitations(ctx, repos, eventID, nil, map[string]bool{attendee.Email: true})
	})
	if err != nil {
		return repository.AttendeeWithName{}, err
	}
	return attendee, nil
}

// AddGroupAttendee invites every current member of groupID as an Attendee
// of eventID, callable by any caller with Editor Access to (or ownership
// of) eventID's Calendar (#162, ADR-0046). This is a one-time snapshot
// expansion, not a dynamic Group Share (ADR-0045): it inserts one attendees
// row per member of groupID as it stands right now (expandGroupMembers'
// shared expansion), and a later change to groupID's membership does not
// retroactively add or remove Attendees this call created, since attendees
// rows carry no link back to the Group they were invited through. groupID
// must belong to eventID's Calendar's own Workspace, mirroring AddAttendee's
// target check. Re-sends to every other current Attendee, same as
// AddAttendee, excluding every member actually added this call — a member
// already an Attendee (expandGroupMembers' own lenient skip) is not
// excluded, since they get no dedicated message of their own to make this
// one redundant.
func (s *EventService) AddGroupAttendee(ctx context.Context, actorUserID int64, eventID string, groupID int64) ([]repository.Attendee, error) {
	event, calendar, err := s.attendeeManagementCalendar(ctx, actorUserID, eventID)
	if err != nil {
		return nil, err
	}

	group, err := s.groups.GetByID(ctx, groupID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrGroupNotFound
		}
		return nil, fmt.Errorf("look up group: %w", err)
	}
	if group.WorkspaceID != calendar.WorkspaceID {
		return nil, ErrAttendeeTargetNotInWorkspace
	}

	var added []repository.Attendee
	err = s.withTx(ctx, func(repos txRepos) error {
		// Reading membership inside the same transaction as the inserts
		// keeps the snapshot honest: a concurrent membership change can't
		// land between "read members" and "write attendees" and end up
		// reflected in neither snapshot.
		var err error
		added, err = expandGroupMembers(ctx, repos.groups, repos.users, repos.attendees, repos.notifications, repos.outbox, event, group.ID, actorUserID, s.inviteRateLimitPerHour)
		if err != nil {
			return err
		}
		if len(added) == 0 {
			return nil
		}
		exceptUserIDs := make(map[int64]bool, len(added))
		for _, a := range added {
			exceptUserIDs[a.UserID] = true
		}
		return s.enqueueReinvitations(ctx, repos, eventID, exceptUserIDs, nil)
	})
	if err != nil {
		return nil, fmt.Errorf("add group attendees: %w", err)
	}

	return added, nil
}

// RemoveAttendee revokes targetUserID's invite to eventID, callable by any
// caller with Editor Access to (or ownership of) eventID's Calendar (#161,
// ADR-0046). Removing their row revokes their visibility to eventID; what
// happens to their historical response is left undecided by ADR-0046, and
// this simply deletes the row. Withdraws their Invitation with a
// METHOD:CANCEL, addressed to them alone, then re-sends to every remaining
// Attendee, unbumped — the Attendee list changing is itself a non-material
// change (ADR-0059, #201).
func (s *EventService) RemoveAttendee(ctx context.Context, actorUserID int64, eventID string, targetUserID int64) error {
	event, _, err := s.attendeeManagementCalendar(ctx, actorUserID, eventID)
	if err != nil {
		return err
	}

	return s.withTx(ctx, func(repos txRepos) error {
		if err := repos.attendees.Remove(ctx, eventID, targetUserID); err != nil {
			return err
		}
		if repos.outbox == nil {
			return nil
		}
		target, err := repos.users.GetByID(ctx, targetUserID)
		if err != nil && !errors.Is(err, repository.ErrNotFound) {
			return fmt.Errorf("look up removed attendee: %w", err)
		}
		if err == nil {
			if err := s.enqueueCancellation(ctx, repos, event, target.Name, target.Email, &targetUserID, nil); err != nil {
				return err
			}
		}
		// targetUserID's own row is already gone from the table by now, so
		// it needs no exclusion here the way AddAttendee's re-send does.
		return s.enqueueReinvitations(ctx, repos, eventID, nil, nil)
	})
}

// RemoveAttendeeByEmail revokes email's Attendee invite to eventID
// (ADR-0058, #200) — the email-shaped counterpart to RemoveAttendee, same
// caller requirement. email is normalized the same way inviteEmail writes
// it, so a differently-cased address still matches the stored row. Withdraws
// their Invitation with a METHOD:CANCEL and re-sends to every remaining
// Attendee, same as RemoveAttendee (ADR-0059, #201).
func (s *EventService) RemoveAttendeeByEmail(ctx context.Context, actorUserID int64, eventID string, email string) error {
	event, _, err := s.attendeeManagementCalendar(ctx, actorUserID, eventID)
	if err != nil {
		return err
	}
	normalized := normalizeEmail(email)

	return s.withTx(ctx, func(repos txRepos) error {
		if err := repos.attendees.RemoveEmail(ctx, eventID, normalized); err != nil {
			return err
		}
		if repos.outbox == nil {
			return nil
		}
		if err := s.enqueueCancellation(ctx, repos, event, "", normalized, nil, &normalized); err != nil {
			return err
		}
		return s.enqueueReinvitations(ctx, repos, eventID, nil, nil)
	})
}

// ListAttendees returns every Attendee of eventID with their Name, for
// display — callable by anyone who can see eventID at all (getVisibleEvent):
// existing Calendar Access, or being an Attendee themselves (#161,
// ADR-0046).
func (s *EventService) ListAttendees(ctx context.Context, userID int64, eventID string) ([]repository.AttendeeWithName, error) {
	if _, err := s.getVisibleEvent(ctx, userID, eventID); err != nil {
		return nil, err
	}
	return s.attendees.ListByEventID(ctx, eventID)
}

// LoadInvitation resolves eventID + recipientUserID into the Event —
// hydrated with its Organizer's Name/Email and its full current Attendee
// list (ADR-0062), but never its Reminders, so icalendar.InvitationToICal
// renders no VALARM (ADR-0059) — and masterAnchor, the Master's own row when
// eventID names an Override (InvitationToICal's RECURRENCE-ID formatting
// contract), nil otherwise. Called by the outbox Worker's Sender with no
// acting User in view, so it skips every Access check getVisibleEvent would
// run — the invite was already authorized when the Attendee row (and this
// Invitation's outbox row) was written.
//
// ok is false when there is nothing left to send: eventID no longer
// resolves, or recipientUserID is no longer one of its Attendees — either
// removed, or the Event deleted, since the outbox row was queued. Neither
// case is an error; it just means the Worker has nothing to do.
func (s *EventService) LoadInvitation(ctx context.Context, eventID string, recipientUserID int64) (event repository.Event, masterAnchor *repository.Event, ok bool, err error) {
	return s.loadInvitationForAttendee(ctx, eventID, func(a repository.AttendeeWithName) bool {
		return a.UserID != nil && *a.UserID == recipientUserID
	})
}

// LoadInvitationForEmail is LoadInvitation's email-shaped counterpart
// (ADR-0058, #200): the same resolution and hydration, but matching the
// current Attendee list on recipientEmail — already normalized by the
// caller — instead of a user_id, since an email-shaped Attendee has no
// account to key on.
func (s *EventService) LoadInvitationForEmail(ctx context.Context, eventID string, recipientEmail string) (event repository.Event, masterAnchor *repository.Event, ok bool, err error) {
	return s.loadInvitationForAttendee(ctx, eventID, func(a repository.AttendeeWithName) bool {
		return a.UserID == nil && a.Email == recipientEmail
	})
}

// loadInvitationForAttendee is LoadInvitation's and LoadInvitationForEmail's
// shared body: resolve eventID, find the current Attendee row matches
// picks out, and hydrate Organizer/masterAnchor around it.
func (s *EventService) loadInvitationForAttendee(ctx context.Context, eventID string, matches func(repository.AttendeeWithName) bool) (event repository.Event, masterAnchor *repository.Event, ok bool, err error) {
	e, err := s.events.GetByID(ctx, eventID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return repository.Event{}, nil, false, nil
		}
		return repository.Event{}, nil, false, fmt.Errorf("get event: %w", err)
	}

	attendees, err := s.attendees.ListByEventID(ctx, eventID)
	if err != nil {
		return repository.Event{}, nil, false, fmt.Errorf("list attendees: %w", err)
	}
	found := false
	for _, a := range attendees {
		if matches(a) {
			found = true
			break
		}
	}
	if !found {
		return repository.Event{}, nil, false, nil
	}
	e.Attendees = attendees

	if e.CreatedBy != nil {
		organizer, err := s.users.GetByID(ctx, *e.CreatedBy)
		if err != nil {
			if !errors.Is(err, repository.ErrNotFound) {
				return repository.Event{}, nil, false, fmt.Errorf("get organizer: %w", err)
			}
		} else {
			e.CreatedByName = organizer.Name
			e.CreatedByEmail = organizer.Email
		}
	}

	if e.ParentID != nil {
		master, err := s.events.GetByID(ctx, *e.ParentID)
		if err != nil {
			return repository.Event{}, nil, false, fmt.Errorf("get master event: %w", err)
		}
		masterAnchor = &master
	}

	return e, masterAnchor, true, nil
}

func isValidResponse(response string) bool {
	switch response {
	case repository.ResponseNeedsAction, repository.ResponseAccepted, repository.ResponseDeclined, repository.ResponseTentative:
		return true
	default:
		return false
	}
}

// SetResponse sets userID's own response to eventID, the only response a
// caller may ever change here — there is no argument naming a different
// target user, so an organizer or Editor managing Attendees can add or
// remove them but can never set their response for them (#161, ADR-0046).
// Returns repository.ErrNotFound if userID isn't an Attendee of eventID.
func (s *EventService) SetResponse(ctx context.Context, userID int64, eventID, response string) (repository.Attendee, error) {
	if !isValidResponse(response) {
		return repository.Attendee{}, ErrInvalidResponse
	}
	if _, err := s.attendees.Get(ctx, eventID, userID); err != nil {
		return repository.Attendee{}, err
	}
	return s.attendees.SetResponse(ctx, eventID, userID, response)
}

// ApplyReply resolves an inbound METHOD:REPLY (icalendar.ParsedReply) and
// writes the Response it names — the reply poller's write path (#202,
// ADR-0059), called with no acting User in view, so it runs no Access check:
// the right to answer for an Attendee row was already established when they
// were invited, the same reasoning loadInvitationForAttendee applies for
// outbound sends. applied is false, with no error, when there is nowhere
// for the reply to go — an unknown UID or an address that isn't an Attendee
// of the resolved row — so the caller logs it and drops it rather than
// guessing (ADR-0059).
func (s *EventService) ApplyReply(ctx context.Context, reply icalendar.ParsedReply) (applied bool, err error) {
	row, err := s.resolveReplyEvent(ctx, reply.UID, reply.RecurrenceID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return false, nil
		}
		return false, fmt.Errorf("resolve reply event: %w", err)
	}

	attendees, err := s.attendees.ListByEventID(ctx, row.ID)
	if err != nil {
		return false, fmt.Errorf("list attendees: %w", err)
	}

	for _, a := range attendees {
		if !strings.EqualFold(a.Email, reply.Attendee) {
			continue
		}
		if a.UserID != nil {
			if _, err := s.attendees.SetResponse(ctx, row.ID, *a.UserID, reply.Response); err != nil {
				return false, fmt.Errorf("set response: %w", err)
			}
		} else if _, err := s.attendees.SetResponseByEmail(ctx, row.ID, a.Email, reply.Response); err != nil {
			return false, fmt.Errorf("set response: %w", err)
		}
		return true, nil
	}
	return false, nil
}

// resolveReplyEvent resolves a REPLY's (UID, RecurrenceID) to the Event row
// that carries the Attendee it answers for: the Override row for that
// Occurrence when one exists (findOverrideForOccurrence, shared with
// GetOccurrence's own resolution so the two can't drift apart on what
// counts as a match), else the Master's own row — since an Attendee invited
// to a whole series has only the Master's row to answer on until that
// Occurrence is split off into its own Override.
func (s *EventService) resolveReplyEvent(ctx context.Context, uid string, recurrenceID *time.Time) (repository.Event, error) {
	master, err := s.events.GetByID(ctx, uid)
	if err != nil {
		return repository.Event{}, err
	}
	if recurrenceID == nil {
		return master, nil
	}

	children, err := s.events.ListChildrenByParentIDs(ctx, []string{uid})
	if err != nil {
		return repository.Event{}, fmt.Errorf("list children: %w", err)
	}
	if override, ok := findOverrideForOccurrence(children[uid], *recurrenceID); ok {
		return override, nil
	}
	return master, nil
}
