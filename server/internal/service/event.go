package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

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
	db         *sql.DB
	events     *repository.EventRepository
	exceptions *repository.EventExceptionRepository
	reminders  *repository.EventReminderRepository
	sync       *repository.SyncRepository
	calendars  *CalendarService
}

func NewEventService(db *sql.DB, events *repository.EventRepository, exceptions *repository.EventExceptionRepository, reminders *repository.EventReminderRepository, sync *repository.SyncRepository, calendars *CalendarService) *EventService {
	return &EventService{db: db, events: events, exceptions: exceptions, reminders: reminders, sync: sync, calendars: calendars}
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
		if write.Rrule != "" || write.RecurrenceID == nil {
			return repository.Event{}, ErrInvalidOverride
		}
		parent, err := s.events.GetByID(ctx, userID, *write.ParentID)
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

	if _, err := s.calendars.Get(ctx, userID, write.CalendarID); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return repository.Event{}, ErrCalendarNotFound
		}
		return repository.Event{}, err
	}

	var event repository.Event
	err := s.withTx(ctx, func(repos txRepos) error {
		seq, err := repos.sync.NextChangeSeq(ctx)
		if err != nil {
			return err
		}
		e, err := repos.events.Create(ctx, id, userID, write.fields(), seq)
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
			if err := repos.events.SetChangeSeq(ctx, userID, *write.ParentID, seq); err != nil {
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
	events, err := s.events.ListByUser(ctx, userID, from, to)
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

// ListAllWithReminders returns every user's Event that carries at least one
// Reminder, across all users — the firing engine's read path (ADR-0021). A
// Master's Reminders come with their own row's ID (needed for the fired
// ledger's exactly-once key) via attachReminders.
func (s *EventService) ListAllWithReminders(ctx context.Context) ([]repository.Event, error) {
	events, err := s.events.ListAllWithReminders(ctx)
	if err != nil {
		return nil, fmt.Errorf("list events with reminders: %w", err)
	}
	if err := s.attachExdates(ctx, events); err != nil {
		return nil, err
	}
	if err := s.attachReminders(ctx, events); err != nil {
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
	exceptionsByParent, err := s.exceptions.ListByParentIDs(ctx, eventIDs(events))
	if err != nil {
		return fmt.Errorf("list exceptions: %w", err)
	}

	for i := range events {
		events[i].Exdates = exceptionsByParent[events[i].ID]
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
	event, err := s.events.GetByID(ctx, userID, id)
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
	if _, err := s.calendars.Get(ctx, userID, write.CalendarID); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return repository.Event{}, ErrCalendarNotFound
		}
		return repository.Event{}, err
	}

	existing, err := s.events.GetByID(ctx, userID, id)
	if err != nil {
		return repository.Event{}, err
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
		u, err := repos.events.Update(ctx, userID, id, write.fields(), seq)
		if err != nil {
			return err
		}
		updated = u

		if discardChildren {
			if err := repos.events.DeleteChildrenOf(ctx, userID, id); err != nil {
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
			if err := repos.events.SetChangeSeq(ctx, userID, *existing.ParentID, seq); err != nil {
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

// GetSeries returns masterID's Master (with Exdates and Reminders attached)
// and its Overrides (each with its own Reminders attached), for CalDAV's
// {masterId}.ics resource (ADR-0025). Only a Master is independently
// addressable, so masterID naming an Override returns ErrParentIsOverride.
func (s *EventService) GetSeries(ctx context.Context, userID int64, masterID string) (repository.Event, []repository.Event, error) {
	master, err := s.events.GetByID(ctx, userID, masterID)
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

	overridesByParent, err := s.events.ListChildrenByParentIDs(ctx, userID, []string{masterID})
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

// ListSeriesByCalendar returns every Master in calendarID (with Exdates and
// Reminders attached) alongside a parentID-keyed map of each Master's
// Overrides (each with its own Reminders attached) — CalDAV's per-calendar
// object listing (ADR-0025).
func (s *EventService) ListSeriesByCalendar(ctx context.Context, userID int64, calendarID string) ([]repository.Event, map[string][]repository.Event, error) {
	masters, err := s.events.ListMastersByCalendar(ctx, userID, calendarID)
	if err != nil {
		return nil, nil, fmt.Errorf("list masters: %w", err)
	}
	if err := s.attachExdates(ctx, masters); err != nil {
		return nil, nil, err
	}

	overridesByParent, err := s.attachOverridesAndReminders(ctx, userID, masters)
	if err != nil {
		return nil, nil, err
	}
	return masters, overridesByParent, nil
}

// attachOverridesAndReminders loads each of masters' Overrides and attaches
// Reminders to both masters and their Overrides in place, returning a
// parentID-keyed map of the Overrides. Shared by every read path that
// recomposes whole series — ListSeriesByCalendar and SyncSince (ADR-0025).
func (s *EventService) attachOverridesAndReminders(ctx context.Context, userID int64, masters []repository.Event) (map[string][]repository.Event, error) {
	masterIDs := eventIDs(masters)
	overridesByParent, err := s.events.ListChildrenByParentIDs(ctx, userID, masterIDs)
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
	masters, err := s.events.ListMastersChangedSince(ctx, userID, calendarID, sinceToken)
	if err != nil {
		return SyncResult{}, fmt.Errorf("list changed masters: %w", err)
	}
	if err := s.attachExdates(ctx, masters); err != nil {
		return SyncResult{}, err
	}

	overridesByParent, err := s.attachOverridesAndReminders(ctx, userID, masters)
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
// (ADR-0025).
func (s *EventService) CalendarCTag(ctx context.Context, calendarID string) (int64, error) {
	return s.sync.CTag(ctx, calendarID)
}

// Delete removes id's row. Deleting a Master removes a whole series, which
// leaves no row for sync-collection to diff against, so its removal is
// recorded as a tombstone instead; deleting an Override still leaves its
// Master's calendar object changed, so the Master's change_seq bumps
// instead (ADR-0025).
func (s *EventService) Delete(ctx context.Context, userID int64, id string) error {
	existing, err := s.events.GetByID(ctx, userID, id)
	if err != nil {
		return err
	}

	return s.withTx(ctx, func(repos txRepos) error {
		if err := repos.events.Delete(ctx, userID, id); err != nil {
			return err
		}

		seq, err := repos.sync.NextChangeSeq(ctx)
		if err != nil {
			return err
		}

		if existing.ParentID == nil {
			return repos.sync.Tombstone(ctx, existing.CalendarID, id, seq)
		}
		if err := repos.events.SetChangeSeq(ctx, userID, *existing.ParentID, seq); err != nil {
			return fmt.Errorf("bump parent change_seq: %w", err)
		}
		return nil
	})
}

// AddException cancels a single Occurrence of a recurring Master (deleting
// "this event"): the rule still generates that slot, but it is suppressed
// from expansion (ADR-0016).
func (s *EventService) AddException(ctx context.Context, userID int64, parentID string, occurrenceStart time.Time) error {
	parent, err := s.events.GetByID(ctx, userID, parentID)
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

	return s.withTx(ctx, func(repos txRepos) error {
		if err := repos.exceptions.Add(ctx, parentID, occurrenceStart); err != nil {
			return fmt.Errorf("add exception: %w", err)
		}
		seq, err := repos.sync.NextChangeSeq(ctx)
		if err != nil {
			return err
		}
		if err := repos.events.SetChangeSeq(ctx, userID, parentID, seq); err != nil {
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
	if _, err := s.events.GetByID(ctx, userID, oldParentID); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return ErrParentNotFound
		}
		return err
	}
	if _, err := s.events.GetByID(ctx, userID, newParentID); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return ErrParentNotFound
		}
		return err
	}

	return s.withTx(ctx, func(repos txRepos) error {
		if err := repos.events.ReparentOverridesFrom(ctx, userID, oldParentID, newParentID, fromStart); err != nil {
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
		if err := repos.events.SetChangeSeq(ctx, userID, oldParentID, seq); err != nil {
			return fmt.Errorf("bump old parent change_seq: %w", err)
		}
		if err := repos.events.SetChangeSeq(ctx, userID, newParentID, seq); err != nil {
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

	if _, err := s.calendars.Get(ctx, userID, calendarID); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return repository.Event{}, nil, ErrCalendarNotFound
		}
		return repository.Event{}, nil, err
	}

	existingMaster, err := s.events.GetByID(ctx, userID, masterID)
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
		overridesByParent, err := s.events.ListChildrenByParentIDs(ctx, userID, []string{masterID})
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
			if _, err := repos.events.Update(ctx, userID, masterID, master, seq); err != nil {
				return fmt.Errorf("update master: %w", err)
			}
		} else {
			if _, err := repos.events.Create(ctx, masterID, userID, master, seq); err != nil {
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
				if _, err := repos.events.Update(ctx, userID, existing.ID, override, seq); err != nil {
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
			if _, err := repos.events.Create(ctx, overrideID, userID, override, seq); err != nil {
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
			if err := repos.events.Delete(ctx, userID, existing.ID); err != nil {
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
