package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

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
	calendars  *CalendarService
}

func NewEventService(db *sql.DB, events *repository.EventRepository, exceptions *repository.EventExceptionRepository, reminders *repository.EventReminderRepository, calendars *CalendarService) *EventService {
	return &EventService{db: db, events: events, exceptions: exceptions, reminders: reminders, calendars: calendars}
}

// withTx runs fn inside a transaction, passing it transaction-bound clones
// of the Event, EventException, and EventReminder repositories so a
// multi-table write commits or rolls back atomically. Reads and validation
// belong outside fn, before withTx is called (ADR-0018).
func (s *EventService) withTx(ctx context.Context, fn func(events *repository.EventRepository, exceptions *repository.EventExceptionRepository, reminders *repository.EventReminderRepository) error) error {
	return repository.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		return fn(s.events.WithTx(tx), s.exceptions.WithTx(tx), s.reminders.WithTx(tx))
	})
}

func (s *EventService) Create(ctx context.Context, userID int64, id, calendarID, title string, start, end time.Time, allDay bool, rrule string, parentID *string, recurrenceID *time.Time, tzid *string, reminders []repository.Reminder) (repository.Event, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return repository.Event{}, ErrInvalidTitle
	}
	if !end.After(start) {
		return repository.Event{}, ErrInvalidTimeRange
	}
	if err := validateReminders(reminders); err != nil {
		return repository.Event{}, err
	}

	if parentID != nil {
		if rrule != "" || recurrenceID == nil {
			return repository.Event{}, ErrInvalidOverride
		}
		parent, err := s.events.GetByID(ctx, userID, *parentID)
		if err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				return repository.Event{}, ErrParentNotFound
			}
			return repository.Event{}, err
		}
		if parent.ParentID != nil {
			return repository.Event{}, ErrParentIsOverride
		}
	} else if !isValidRecurrenceRule(rrule) {
		return repository.Event{}, ErrInvalidRecurrenceRule
	}

	if _, err := s.calendars.Get(ctx, userID, calendarID); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return repository.Event{}, ErrCalendarNotFound
		}
		return repository.Event{}, err
	}

	var event repository.Event
	err := s.withTx(ctx, func(events *repository.EventRepository, exceptions *repository.EventExceptionRepository, remindersRepo *repository.EventReminderRepository) error {
		e, err := events.Create(ctx, id, userID, calendarID, title, start, end, allDay, rrule, parentID, recurrenceID, tzid)
		if err != nil {
			return err
		}
		if err := remindersRepo.ReplaceByEventID(ctx, e.ID, reminders); err != nil {
			return fmt.Errorf("persist reminders: %w", err)
		}
		event = e
		return nil
	})
	if err != nil {
		return repository.Event{}, fmt.Errorf("create event: %w", err)
	}
	event.Reminders = reminders
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

func (s *EventService) attachExdates(ctx context.Context, events []repository.Event) error {
	ids := make([]string, 0, len(events))
	for _, e := range events {
		ids = append(ids, e.ID)
	}

	exceptionsByParent, err := s.exceptions.ListByParentIDs(ctx, ids)
	if err != nil {
		return fmt.Errorf("list exceptions: %w", err)
	}

	for i := range events {
		events[i].Exdates = exceptionsByParent[events[i].ID]
	}
	return nil
}

func (s *EventService) attachReminders(ctx context.Context, events []repository.Event) error {
	ids := make([]string, 0, len(events))
	for _, e := range events {
		ids = append(ids, e.ID)
	}

	remindersByEvent, err := s.reminders.ListByEventIDs(ctx, ids)
	if err != nil {
		return fmt.Errorf("list reminders: %w", err)
	}

	for i := range events {
		events[i].Reminders = remindersByEvent[events[i].ID]
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

func (s *EventService) Update(ctx context.Context, userID int64, id, calendarID, title string, start, end time.Time, allDay bool, rrule string, tzid *string, reminders []repository.Reminder) (repository.Event, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return repository.Event{}, ErrInvalidTitle
	}
	if !end.After(start) {
		return repository.Event{}, ErrInvalidTimeRange
	}
	if !isValidRecurrenceRule(rrule) {
		return repository.Event{}, ErrInvalidRecurrenceRule
	}
	if err := validateReminders(reminders); err != nil {
		return repository.Event{}, err
	}
	if _, err := s.calendars.Get(ctx, userID, calendarID); err != nil {
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
	discardChildren := existing.ParentID == nil && !samePattern(existing.Rrule, rrule)

	var updated repository.Event
	err = s.withTx(ctx, func(events *repository.EventRepository, exceptions *repository.EventExceptionRepository, remindersRepo *repository.EventReminderRepository) error {
		u, err := events.Update(ctx, userID, id, calendarID, title, start, end, allDay, rrule, tzid)
		if err != nil {
			return err
		}
		updated = u

		if discardChildren {
			if err := events.DeleteChildrenOf(ctx, userID, id); err != nil {
				return fmt.Errorf("discard overrides: %w", err)
			}
			if err := exceptions.DeleteByParentID(ctx, id); err != nil {
				return fmt.Errorf("discard exceptions: %w", err)
			}
		}

		if err := remindersRepo.ReplaceByEventID(ctx, id, reminders); err != nil {
			return fmt.Errorf("persist reminders: %w", err)
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

func (s *EventService) Delete(ctx context.Context, userID int64, id string) error {
	return s.events.Delete(ctx, userID, id)
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

	if err := s.exceptions.Add(ctx, parentID, occurrenceStart); err != nil {
		return fmt.Errorf("add exception: %w", err)
	}
	return nil
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

	return s.withTx(ctx, func(events *repository.EventRepository, exceptions *repository.EventExceptionRepository, reminders *repository.EventReminderRepository) error {
		if err := events.ReparentOverridesFrom(ctx, userID, oldParentID, newParentID, fromStart); err != nil {
			return fmt.Errorf("reparent overrides: %w", err)
		}
		if err := exceptions.ReparentFrom(ctx, oldParentID, newParentID, fromStart); err != nil {
			return fmt.Errorf("reparent exceptions: %w", err)
		}
		return nil
	})
}
