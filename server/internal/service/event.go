package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/XiovV/calich/server/internal/repository"
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
	// read-only-mode Source, whose Events are written only by Refresh's
	// bypass, ImportSubscribedSeries, for Owner and Editor alike (ADR-0032,
	// ADR-0052). The guard lives here rather than at the REST/CalDAV edges
	// so every entry point is covered by construction.
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

// updateEffects is Update's classification of existing against write,
// computed once by classifyUpdate and then just applied — the transaction
// body reads as effects taken, not booleans re-derived at each branch.
type updateEffects struct {
	// discardChildren forces "All events" on a Master whose rule pattern
	// changed (or was removed), discarding its existing Overrides/Exceptions
	// (ADR-0016) — see classifyUpdate.
	discardChildren bool
	// newSequence is row's iTIP SEQUENCE to write — existing.Sequence,
	// incremented when the change is material (ADR-0059).
	newSequence int64
	// contentChanged widens material to everything an Invitation actually
	// renders — title, description, location and colour (ADR-0063) on top of
	// the material set — and gates Update's re-send to every current
	// Attendee, bumped or not.
	contentChanged bool
}

// classifyUpdate computes Update's updateEffects from existing against
// write. A Master's rule changing (or being removed) is forced to "All
// events" and discards existing Overrides/Exceptions, because their
// recurrence ids may no longer be generated by the new rule (ADR-0016); the
// frontend warns before calling this, and a non-recurring Master or an
// unchanged rule has nothing to discard. material is the ADR-0059 change set
// that resets a recipient's PARTSTAT — start, end, rrule, all_day — and is
// the only thing that bumps row's own iTIP sequence.
func classifyUpdate(existing repository.Event, write EventWrite) updateEffects {
	discardChildren := existing.ParentID == nil && !samePattern(existing.Rrule, write.Rrule)

	material := materialFieldsChanged(existing, write)
	newSequence := existing.Sequence
	if material {
		newSequence++
	}
	contentChanged := material || existing.Title != write.Title || existing.Description != write.Description || existing.Location != write.Location || existing.URL != write.URL || !colorEqual(existing.Color, write.Color)

	return updateEffects{
		discardChildren: discardChildren,
		newSequence:     newSequence,
		contentChanged:  contentChanged,
	}
}

type EventService struct {
	db         *sql.DB
	events     *repository.EventRepository
	exceptions *repository.EventExceptionRepository
	reminders  *repository.EventReminderRepository
	// calendarDefaults and explicitReminders are ADR-0064's Default
	// reminders machinery: calendarDefaults holds each User's own timed/
	// all-day default lists per Calendar, explicitReminders records that a
	// User's Reminder list on one Event is explicit (even if empty), so
	// resolution knows when to stop at "nothing" instead of falling back to
	// the default. Held here for the write paths; every *read* of a Reminder
	// goes through reminderResolution instead.
	calendarDefaults  *repository.CalendarDefaultReminderRepository
	explicitReminders *repository.EventReminderExplicitRepository
	// reminderResolution is the one place ADR-0064's resolution rule is
	// stated (#216) — the viewer read and the firing engine's every-User read
	// are one call each into it.
	reminderResolution *reminderResolver
	sync               *repository.SyncRepository
	calendars          *CalendarService
	users              *repository.UserRepository
	attachments        *repository.AttachmentRepository
	attendees          *repository.AttendeeRepository
	workspaces         *repository.WorkspaceRepository
	groups             *repository.GroupRepository
	notifications      *repository.NotificationRepository
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

func NewEventService(db *sql.DB, events *repository.EventRepository, exceptions *repository.EventExceptionRepository, reminders *repository.EventReminderRepository, calendarDefaults *repository.CalendarDefaultReminderRepository, explicitReminders *repository.EventReminderExplicitRepository, sync *repository.SyncRepository, calendars *CalendarService, users *repository.UserRepository, attachments *repository.AttachmentRepository, attendees *repository.AttendeeRepository, workspaces *repository.WorkspaceRepository, groups *repository.GroupRepository, notifications *repository.NotificationRepository, outbox *repository.OutboxRepository, inviteRateLimitPerHour int) *EventService {
	return &EventService{db: db, events: events, exceptions: exceptions, reminders: reminders, calendarDefaults: calendarDefaults, explicitReminders: explicitReminders, reminderResolution: &reminderResolver{reminders: reminders, explicit: explicitReminders, calendarDefaults: calendarDefaults}, sync: sync, calendars: calendars, users: users, attachments: attachments, attendees: attendees, workspaces: workspaces, groups: groups, notifications: notifications, outbox: outbox, inviteRateLimitPerHour: inviteRateLimitPerHour}
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
// Viewer Share (ADR-0034), and, per ADR-0032/ADR-0052's clamp, for a
// Calendar with a read-only-mode Source, Owner and Editor alike (Viewer) —
// in one call: the guard every mutating method except
// ImportSubscribedSeries applies to every Calendar its write touches. This
// is the Source write guard expressed through the Access resolver rather
// than beside it: ResolveAccess is what decides read-only, keyed off the
// Source's Mode, not a check made here.
//
// Returns the Calendar the Access resolution already fetched, so a caller
// that also needs it — its WorkspaceID, typically — reads it here instead of
// paying calendarByID's up-to-three queries to resolve the same Access a
// second time. Guard-only callers discard it explicitly.
func (s *EventService) requireWritableCalendar(ctx context.Context, userID int64, calendarID string) (repository.Calendar, error) {
	access, calendar, err := s.calendars.Access(ctx, userID, calendarID)
	if err != nil {
		return repository.Calendar{}, asCalendarNotFound(err)
	}
	if !access.CanRead() {
		return repository.Calendar{}, ErrCalendarNotFound
	}
	if !access.CanWrite() {
		return repository.Calendar{}, ErrCalendarReadOnly
	}
	return calendar, nil
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
	events     *repository.EventRepository
	exceptions *repository.EventExceptionRepository
	reminders  *repository.EventReminderRepository
	// explicitReminders is Create's Override/split-copy path alone
	// (ADR-0064): copying explicit markers alongside reminders.CopyByEventID
	// so an opt-out on the Master survives onto the new row.
	explicitReminders *repository.EventReminderExplicitRepository
	sync              *repository.SyncRepository
	attachments       *repository.AttachmentRepository
	attendees         *repository.AttendeeRepository
	groups            *repository.GroupRepository
	users             *repository.UserRepository
	workspaces        *repository.WorkspaceRepository
	notifications     *repository.NotificationRepository
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
			events:            s.events.WithTx(tx),
			exceptions:        s.exceptions.WithTx(tx),
			reminders:         s.reminders.WithTx(tx),
			explicitReminders: s.explicitReminders.WithTx(tx),
			sync:              s.sync.WithTx(tx),
			attachments:       s.attachments.WithTx(tx),
			attendees:         s.attendees.WithTx(tx),
			groups:            s.groups.WithTx(tx),
			users:             s.users.WithTx(tx),
			workspaces:        s.workspaces.WithTx(tx),
			notifications:     s.notifications.WithTx(tx),
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

	calendar, err := s.requireWritableCalendar(ctx, userID, write.CalendarID)
	if err != nil {
		return repository.Event{}, err
	}

	// Taken off the Calendar the guard above already resolved (#187 paid three
	// queries for it, and only when there was an Attendee to check against it;
	// a field read is worth no such condition). Read only by
	// addCreateAttendees' loops, so a Create with no Attendee never looks at it.
	workspaceID := calendar.WorkspaceID

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
			if err := repos.explicitReminders.CopyByEventID(ctx, *copyFrom, e.ID); err != nil {
				return fmt.Errorf("copy explicit reminder markers: %w", err)
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
	// The new row's Reminders come back resolved (ADR-0064) like every other
	// field, through the same recipe Get answers with: whatever
	// CopyRemindersFrom just copied, if any, otherwise userID's matching
	// Calendar default — a plain create pre-fills no explicit rows, so it
	// picks up the creator's own default like any other Event.
	result := []repository.Event{event}
	if err := s.hydrateEvents(ctx, eventPointers(result), userID, restEventFields); err != nil {
		return repository.Event{}, err
	}
	return result[0], nil
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
	if err := s.hydrateEvents(ctx, eventPointers(events), userID, restEventFields.withKnownCalendars(knownCalendars)); err != nil {
		return nil, err
	}
	return events, nil
}

// mergeEventsByID appends b's Events not already present in a (by id), for the
// two reads assembled from halves that can overlap: List's Calendar-Access and
// Attendee sets, when a caller has both to the same Event (ADR-0046), and
// ListAllWithReminders' Reminder-row and Calendar-default candidates, and its
// shadowing Overrides on top of both. Either way the merged result must carry
// each Event exactly once.
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

// eventPointers takes the rows of a contiguous slice into hydrateEvents' row
// form. It aliases events rather than copying it, so hydration fills in the
// caller's own rows.
func eventPointers(events []repository.Event) []*repository.Event {
	rows := make([]*repository.Event, len(events))
	for i := range events {
		rows[i] = &events[i]
	}
	return rows
}

// Get returns id if userID can see it — via Calendar Access or, failing
// that, as one of its Attendees (ADR-0046, #161).
func (s *EventService) Get(ctx context.Context, userID int64, id string) (repository.Event, error) {
	event, err := s.getVisibleEvent(ctx, userID, id)
	if err != nil {
		return repository.Event{}, err
	}
	events := []repository.Event{event}
	if err := s.hydrateEvents(ctx, eventPointers(events), userID, restEventFields); err != nil {
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
	if _, err := s.requireWritableCalendar(ctx, userID, write.CalendarID); err != nil {
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
		if _, err := s.requireWritableCalendar(ctx, userID, existing.CalendarID); err != nil {
			return repository.Event{}, err
		}
	}

	if overrideCarriesOwnRrule(existing.ParentID, write.Rrule) {
		return repository.Event{}, ErrInvalidOverride
	}

	// See classifyUpdate's own doc comment for what each effect means and
	// when it fires.
	effects := classifyUpdate(existing, write)

	var updated repository.Event
	err = s.withTx(ctx, func(repos txRepos) error {
		seq, err := repos.sync.NextChangeSeq(ctx)
		if err != nil {
			return err
		}
		u, err := repos.events.Update(ctx, id, write.fields(), seq, effects.newSequence)
		if err != nil {
			return err
		}
		updated = u

		if effects.discardChildren {
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

		if effects.contentChanged {
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
	if err := s.hydrateEvents(ctx, eventPointers(result), userID, restEventFields); err != nil {
		return repository.Event{}, err
	}
	return result[0], nil
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
	if _, err := s.requireWritableCalendar(ctx, userID, existing.CalendarID); err != nil {
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
	if _, err := s.requireWritableCalendar(ctx, userID, parent.CalendarID); err != nil {
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

	if _, err := s.requireWritableCalendar(ctx, userID, oldParent.CalendarID); err != nil {
		return err
	}
	if newParent.CalendarID != oldParent.CalendarID {
		if _, err := s.requireWritableCalendar(ctx, userID, newParent.CalendarID); err != nil {
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
