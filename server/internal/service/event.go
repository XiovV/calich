package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

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
	// ErrCalendarReadOnly is returned by every mutating method (Create,
	// Update, Delete, AddException, ReparentFrom, ImportSeries, PutSeries)
	// when the caller's Access to the Calendar a write targets doesn't
	// clear CanWrite — a Viewer Share (ADR-0034), or a Calendar carrying a
	// SourceURL, whose Events are written only by Refresh's bypass,
	// ImportSubscribedSeries, for Owner and Editor alike (ADR-0032). The
	// guard lives here rather than at the REST/CalDAV edges so every entry
	// point is covered by construction.
	ErrCalendarReadOnly = errors.New("calendar is read-only")
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

type EventService struct {
	db                *sql.DB
	events            *repository.EventRepository
	exceptions        *repository.EventExceptionRepository
	reminders         *repository.EventReminderRepository
	reminderOverrides *repository.ReminderOverrideRepository
	sync              *repository.SyncRepository
	calendars         *CalendarService
}

func NewEventService(db *sql.DB, events *repository.EventRepository, exceptions *repository.EventExceptionRepository, reminders *repository.EventReminderRepository, reminderOverrides *repository.ReminderOverrideRepository, sync *repository.SyncRepository, calendars *CalendarService) *EventService {
	return &EventService{db: db, events: events, exceptions: exceptions, reminders: reminders, reminderOverrides: reminderOverrides, sync: sync, calendars: calendars}
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

// ReminderOverrideWrite is a Reminder override's writable fields (ADR-0036):
// OffsetMinutes and Channel are independently nil to leave the Event's own
// value in effect for that dimension; Muted, when true, silences every
// Reminder on the Event for the caller regardless of the other two.
type ReminderOverrideWrite struct {
	OffsetMinutes *int
	Channel       *string
	Muted         bool
}

// GetReminderOverride returns userID's own Reminder override on eventID.
// found is false when userID has never set one — the fallback case where the
// Event's own Reminders apply unchanged — which is a normal outcome here,
// not an error; only a missing/inaccessible eventID returns an error
// (ADR-0036). Lets a client read the current override back before changing
// just one of its fields, since SetReminderOverride replaces it wholesale.
func (s *EventService) GetReminderOverride(ctx context.Context, userID int64, eventID string) (override repository.ReminderOverride, found bool, err error) {
	if _, err := s.getOwnedEvent(ctx, userID, eventID); err != nil {
		return repository.ReminderOverride{}, false, err
	}

	override, err = s.reminderOverrides.Get(ctx, userID, eventID)
	if errors.Is(err, repository.ErrNotFound) {
		return repository.ReminderOverride{}, false, nil
	}
	if err != nil {
		return repository.ReminderOverride{}, false, fmt.Errorf("get reminder override: %w", err)
	}
	return override, true, nil
}

// SetReminderOverride sets userID's own Reminder override on eventID —
// retuning the offset, the Channel, or both, or muting the Event's Reminders
// entirely, for userID alone (ADR-0036). Any User with at least Viewer
// Access to eventID's Calendar may call this — getOwnedEvent's CanRead
// check, not requireWritableCalendar's CanWrite one, since an override is a
// personal delivery preference rather than a write to the Event itself. It
// never touches the Event, its Reminders, or its change sequence.
func (s *EventService) SetReminderOverride(ctx context.Context, userID int64, eventID string, write ReminderOverrideWrite) (repository.ReminderOverride, error) {
	if write.Channel != nil && !isValidReminderChannel(*write.Channel) {
		return repository.ReminderOverride{}, ErrInvalidReminderChannel
	}
	if _, err := s.getOwnedEvent(ctx, userID, eventID); err != nil {
		return repository.ReminderOverride{}, err
	}

	override, err := s.reminderOverrides.Upsert(ctx, userID, eventID, repository.ReminderOverride{
		OffsetMinutes: write.OffsetMinutes,
		Channel:       write.Channel,
		Muted:         write.Muted,
	})
	if err != nil {
		return repository.ReminderOverride{}, fmt.Errorf("set reminder override: %w", err)
	}
	return override, nil
}

// ClearReminderOverride removes userID's own Reminder override on eventID,
// falling back to the Event's own Reminders (ADR-0036). A no-op, not an
// error, if userID never set one.
func (s *EventService) ClearReminderOverride(ctx context.Context, userID int64, eventID string) error {
	if _, err := s.getOwnedEvent(ctx, userID, eventID); err != nil {
		return err
	}
	return s.reminderOverrides.Delete(ctx, userID, eventID)
}

// txRepos is the set of transaction-bound repositories a withTx body writes
// through. Grouped into one struct so a body names only what it needs — most
// use two or three of the four.
type txRepos struct {
	events     *repository.EventRepository
	exceptions *repository.EventExceptionRepository
	reminders  *repository.EventReminderRepository
	sync       *repository.SyncRepository
}

// withTx runs fn inside a transaction, passing it transaction-bound clones
// of the Event, EventException, EventReminder, and Sync repositories so a
// multi-table write — including its change_seq bump — commits or rolls back
// atomically. Reads and validation belong outside fn, before withTx is
// called (ADR-0018).
func (s *EventService) withTx(ctx context.Context, fn func(repos txRepos) error) error {
	return repository.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		return fn(txRepos{
			events:     s.events.WithTx(tx),
			exceptions: s.exceptions.WithTx(tx),
			reminders:  s.reminders.WithTx(tx),
			sync:       s.sync.WithTx(tx),
		})
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
	Reminders    []repository.Reminder
	Description  string
	Location     string
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
	if err := validateReminders(write.Reminders); err != nil {
		return repository.Event{}, err
	}

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

	var event repository.Event
	err := s.withTx(ctx, func(repos txRepos) error {
		seq, err := repos.sync.NextChangeSeq(ctx)
		if err != nil {
			return err
		}
		e, err := repos.events.Create(ctx, id, &userID, write.fields(), seq)
		if err != nil {
			return err
		}
		if err := repos.reminders.ReplaceByEventID(ctx, e.ID, write.Reminders); err != nil {
			return fmt.Errorf("persist reminders: %w", err)
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
	event.Reminders = write.Reminders
	return event, nil
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

// List returns a user's events with each Master's Exdates populated from its
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
	if err := s.attachExdates(ctx, events); err != nil {
		return nil, err
	}
	if err := s.attachReminders(ctx, events); err != nil {
		return nil, err
	}
	return events, nil
}

// ListAllWithReminders returns every Event that carries at least one
// Reminder, across every Calendar, alongside its Calendar's Owner — the
// firing engine's read path (ADR-0021). A Master's Reminders come with
// their own row's ID (needed for the fired ledger's exactly-once key) via
// attachReminders.
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
	if err := s.attachRemindersTo(ctx, pointers); err != nil {
		return nil, err
	}
	return events, nil
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
// memory, mirroring attachRemindersTo.
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

func (s *EventService) attachReminders(ctx context.Context, events []repository.Event) error {
	pointers := make([]*repository.Event, len(events))
	for i := range events {
		pointers[i] = &events[i]
	}
	return s.attachRemindersTo(ctx, pointers)
}

// attachRemindersTo fills in Reminders on rows that need not be contiguous in
// memory — a series spans one slice for its Master and another for its
// Overrides — while still batching a single lookup across all of them.
func (s *EventService) attachRemindersTo(ctx context.Context, events []*repository.Event) error {
	ids := make([]string, 0, len(events))
	for _, e := range events {
		ids = append(ids, e.ID)
	}

	remindersByEvent, err := s.reminders.ListByEventIDs(ctx, ids)
	if err != nil {
		return fmt.Errorf("list reminders: %w", err)
	}

	for _, e := range events {
		e.Reminders = remindersByEvent[e.ID]
	}
	return nil
}

func (s *EventService) Get(ctx context.Context, userID int64, id string) (repository.Event, error) {
	event, err := s.getOwnedEvent(ctx, userID, id)
	if err != nil {
		return repository.Event{}, err
	}
	events := []repository.Event{event}
	if err := s.attachExdates(ctx, events); err != nil {
		return repository.Event{}, err
	}
	if err := s.attachReminders(ctx, events); err != nil {
		return repository.Event{}, err
	}
	return events[0], nil
}

// Update rewrites id's fields. write.ParentID and write.RecurrenceID are
// ignored — see EventWrite.
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
	if err := validateReminders(write.Reminders); err != nil {
		return repository.Event{}, err
	}
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

	var updated repository.Event
	err = s.withTx(ctx, func(repos txRepos) error {
		seq, err := repos.sync.NextChangeSeq(ctx)
		if err != nil {
			return err
		}
		u, err := repos.events.Update(ctx, id, write.fields(), seq)
		if err != nil {
			return err
		}
		updated = u

		if discardChildren {
			if err := repos.events.DeleteChildrenOf(ctx, id); err != nil {
				return fmt.Errorf("discard overrides: %w", err)
			}
			if err := repos.exceptions.DeleteByParentID(ctx, id); err != nil {
				return fmt.Errorf("discard exceptions: %w", err)
			}
		}

		if err := repos.reminders.ReplaceByEventID(ctx, id, write.Reminders); err != nil {
			return fmt.Errorf("persist reminders: %w", err)
		}

		// Updating an Override changes its Master's calendar object, so the
		// Master's own change_seq must bump too (ADR-0025).
		if existing.ParentID != nil {
			if err := repos.events.SetChangeSeq(ctx, *existing.ParentID, seq); err != nil {
				return fmt.Errorf("bump parent change_seq: %w", err)
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
	if err := s.attachReminders(ctx, result); err != nil {
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

	return s.writeSeries(ctx, userID, calendarID, writes)
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

	return s.writeSeries(ctx, userID, calendarID, writes)
}

// writeSeries is ImportSeries and ImportSubscribedSeries' shared insert-only
// write, once each has resolved and (except for the bypass) guarded
// calendarID.
func (s *EventService) writeSeries(ctx context.Context, userID int64, calendarID string, writes []SeriesWrite) (int, error) {
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
				ExternalUID: nonEmptyPtr(w.ExternalUID),
			}
			if _, err := repos.events.Create(ctx, masterID, &userID, master, seq); err != nil {
				return fmt.Errorf("create master: %w", err)
			}
			if err := repos.reminders.ReplaceByEventID(ctx, masterID, w.Reminders); err != nil {
				return fmt.Errorf("persist master reminders: %w", err)
			}
			for _, exdate := range w.Exdates {
				if err := repos.exceptions.Add(ctx, masterID, exdate); err != nil {
					return fmt.Errorf("add exdate: %w", err)
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
					ParentID:     &masterID,
					RecurrenceID: &o.RecurrenceID,
					ExternalUID:  nonEmptyPtr(o.ExternalUID),
				}
				overrideID := uuid.NewString()
				if _, err := repos.events.Create(ctx, overrideID, &userID, override, seq); err != nil {
					return fmt.Errorf("create override: %w", err)
				}
				if err := repos.reminders.ReplaceByEventID(ctx, overrideID, o.Reminders); err != nil {
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

	if _, err := s.calendarByID(ctx, userID, calendarID); err != nil {
		return ReconcileSummary{}, err
	}

	var summary ReconcileSummary
	err := s.withTx(ctx, func(repos txRepos) error {
		for _, upsert := range result.Upserts {
			if upsert.MasterID == "" {
				if err := s.createSubscribedSeries(ctx, repos, userID, calendarID, upsert.Write); err != nil {
					return err
				}
				summary.Created++
				continue
			}
			if err := s.updateSubscribedSeries(ctx, repos, userID, calendarID, upsert.MasterID, upsert.Write); err != nil {
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
func (s *EventService) createSubscribedSeries(ctx context.Context, repos txRepos, userID int64, calendarID string, write SeriesWrite) error {
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
		ExternalUID: nonEmptyPtr(write.ExternalUID),
	}
	if _, err := repos.events.Create(ctx, masterID, &userID, master, seq); err != nil {
		return fmt.Errorf("create master: %w", err)
	}
	if err := repos.reminders.ReplaceByEventID(ctx, masterID, write.Reminders); err != nil {
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
			ParentID:     &masterID,
			RecurrenceID: &o.RecurrenceID,
			ExternalUID:  nonEmptyPtr(o.ExternalUID),
		}
		overrideID := uuid.NewString()
		if _, err := repos.events.Create(ctx, overrideID, &userID, override, seq); err != nil {
			return fmt.Errorf("create override: %w", err)
		}
		if err := repos.reminders.ReplaceByEventID(ctx, overrideID, o.Reminders); err != nil {
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
// exactly as createSubscribedSeries does.
func (s *EventService) updateSubscribedSeries(ctx context.Context, repos txRepos, userID int64, calendarID, masterID string, write SeriesWrite) error {
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
	}
	if _, err := repos.events.Update(ctx, masterID, master, seq); err != nil {
		return fmt.Errorf("update master: %w", err)
	}
	if err := repos.reminders.ReplaceByEventID(ctx, masterID, write.Reminders); err != nil {
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
		}

		if existing, ok := existingByRecurrenceID[key]; ok {
			if _, err := repos.events.Update(ctx, existing.ID, override, seq); err != nil {
				return fmt.Errorf("update override: %w", err)
			}
			if err := repos.reminders.ReplaceByEventID(ctx, existing.ID, o.Reminders); err != nil {
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
		if err := repos.reminders.ReplaceByEventID(ctx, overrideID, o.Reminders); err != nil {
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

// ClearSubscribedCalendarReminders deletes every Reminder attached to
// calendarID's Events — Masters and Overrides alike — the immediate
// consequence of a Subscription's KeepAlarms being turned off (#87,
// ADR-0032): "off" must mean no Reminders exist, not merely that a future
// Refresh will stop adding them. Bumps each Master's change_seq once so a
// CalDAV client sees the alarm-less object on its next sync, mirroring how
// an Override's own change is reflected on its Master (ADR-0025). This is
// ImportSubscribedSeries and ReconcileSubscribedSeries' third sibling
// bypass of the Subscribed Calendar write guard (ADR-0032) — clearing a
// Subscription's own Reminders in its own Subscribed Calendar is a
// legitimate write.
func (s *EventService) ClearSubscribedCalendarReminders(ctx context.Context, userID int64, calendarID string) error {
	if _, err := s.calendarByID(ctx, userID, calendarID); err != nil {
		return err
	}

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
			if err := repos.reminders.ReplaceByEventID(ctx, m.ID, nil); err != nil {
				return fmt.Errorf("clear master reminders: %w", err)
			}
			for _, o := range overridesByParent[m.ID] {
				if err := repos.reminders.ReplaceByEventID(ctx, o.ID, nil); err != nil {
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

	masters := []repository.Event{master}
	if err := s.attachExdates(ctx, masters); err != nil {
		return repository.Event{}, nil, err
	}

	overridesByParent, err := s.events.ListChildrenByParentIDs(ctx, []string{masterID})
	if err != nil {
		return repository.Event{}, nil, fmt.Errorf("list overrides: %w", err)
	}
	overrides := overridesByParent[masterID]

	all := append(masters, overrides...)
	if err := s.attachReminders(ctx, all); err != nil {
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

	for _, override := range overrides {
		if override.RecurrenceID != nil && override.RecurrenceID.Equal(occurrenceStart) {
			flattened := override
			flattened.ParentID = nil
			flattened.RecurrenceID = nil
			// An Override never carries a rule of its own (ADR-0016), so
			// this is always already "", but cleared explicitly since
			// icalendar.OccurrenceToICal trusts its caller to have done so.
			flattened.Rrule = ""
			return flattened, nil
		}
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
	if _, err := s.calendarByID(ctx, userID, calendarID); err != nil {
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

	overridesByParent, err := s.attachOverridesAndReminders(ctx, masters)
	if err != nil {
		return nil, nil, err
	}
	return masters, overridesByParent, nil
}

// attachOverridesAndReminders loads each of masters' Overrides and attaches
// Reminders to both masters and their Overrides in place, returning a
// parentID-keyed map of the Overrides. Shared by every read path that
// recomposes whole series — ListSeriesByCalendar and SyncSince (ADR-0025).
// Callers have already checked the caller's Access to masters' Calendar, so
// this needs no userID of its own.
func (s *EventService) attachOverridesAndReminders(ctx context.Context, masters []repository.Event) (map[string][]repository.Event, error) {
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
	if err := s.attachRemindersTo(ctx, all); err != nil {
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
	if _, err := s.calendarByID(ctx, userID, calendarID); err != nil {
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

	overridesByParent, err := s.attachOverridesAndReminders(ctx, masters)
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

// Delete removes id's row. Deleting a Master removes a whole series, which
// leaves no row for sync-collection to diff against, so its removal is
// recorded as a tombstone instead; deleting an Override still leaves its
// Master's calendar object changed, so the Master's change_seq bumps
// instead (ADR-0025).
func (s *EventService) Delete(ctx context.Context, userID int64, id string) error {
	existing, err := s.getOwnedEvent(ctx, userID, id)
	if err != nil {
		return err
	}
	if err := s.requireWritableCalendar(ctx, userID, existing.CalendarID); err != nil {
		return err
	}

	return s.withTx(ctx, func(repos txRepos) error {
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
// belong to the caller.
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
	Title, Description, Location string
	Start, End                   time.Time
	AllDay                       bool
	Tzid                         *string
	Rrule                        string
	Reminders                    []repository.Reminder
	Exdates                      []time.Time
	Overrides                    []OverrideWrite
	// ExternalUID is set only when ImportSeries is writing a Subscribed
	// Calendar's Events (#83, ADR-0033) — empty for ordinary import and
	// CalDAV PUT, which leave the row's external_uid column NULL.
	ExternalUID string
}

// OverrideWrite is one Override VEVENT's fields, keyed by the Occurrence it
// replaces (RecurrenceID).
type OverrideWrite struct {
	RecurrenceID                 time.Time
	Title, Description, Location string
	Start, End                   time.Time
	AllDay                       bool
	Tzid                         *string
	Reminders                    []repository.Reminder
	// ExternalUID mirrors SeriesWrite.ExternalUID — an Override shares its
	// Master's foreign UID (#83, ADR-0033).
	ExternalUID string
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
		for j, o := range w.Overrides {
			trimmed, err := validateEventFields(o.Title, o.Start, o.End, o.Reminders)
			if err != nil {
				return err
			}
			writes[i].Overrides[j].Title = trimmed
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
	for i, o := range write.Overrides {
		trimmed, err := validateEventFields(o.Title, o.Start, o.End, o.Reminders)
		if err != nil {
			return repository.Event{}, nil, err
		}
		write.Overrides[i].Title = trimmed
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
		}
		if masterExists {
			if _, err := repos.events.Update(ctx, masterID, master, seq); err != nil {
				return fmt.Errorf("update master: %w", err)
			}
		} else {
			if _, err := repos.events.Create(ctx, masterID, &userID, master, seq); err != nil {
				return fmt.Errorf("create master: %w", err)
			}
		}
		if err := repos.reminders.ReplaceByEventID(ctx, masterID, write.Reminders); err != nil {
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
			}

			if existing, ok := existingByRecurrenceID[key]; ok {
				if _, err := repos.events.Update(ctx, existing.ID, override, seq); err != nil {
					return fmt.Errorf("update override: %w", err)
				}
				if err := repos.reminders.ReplaceByEventID(ctx, existing.ID, o.Reminders); err != nil {
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
			if err := repos.reminders.ReplaceByEventID(ctx, overrideID, o.Reminders); err != nil {
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
