package service

import (
	"context"
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
)

type EventService struct {
	events     *repository.EventRepository
	exceptions *repository.EventExceptionRepository
	calendars  *CalendarService
}

func NewEventService(events *repository.EventRepository, exceptions *repository.EventExceptionRepository, calendars *CalendarService) *EventService {
	return &EventService{events: events, exceptions: exceptions, calendars: calendars}
}

func (s *EventService) Create(ctx context.Context, userID int64, id, calendarID, title string, start, end time.Time, allDay bool, rrule string, parentID *string, recurrenceID *time.Time) (repository.Event, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return repository.Event{}, ErrInvalidTitle
	}
	if !end.After(start) {
		return repository.Event{}, ErrInvalidTimeRange
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

	event, err := s.events.Create(ctx, id, userID, calendarID, title, start, end, allDay, rrule, parentID, recurrenceID)
	if err != nil {
		return repository.Event{}, fmt.Errorf("create event: %w", err)
	}
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
// Exceptions (ADR-0016). Overrides always have an empty Exdates.
func (s *EventService) List(ctx context.Context, userID int64, from, to *time.Time) ([]repository.Event, error) {
	events, err := s.events.ListByUser(ctx, userID, from, to)
	if err != nil {
		return nil, fmt.Errorf("list events: %w", err)
	}
	if err := s.attachExdates(ctx, events); err != nil {
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

func (s *EventService) Get(ctx context.Context, userID int64, id string) (repository.Event, error) {
	event, err := s.events.GetByID(ctx, userID, id)
	if err != nil {
		return repository.Event{}, err
	}
	events := []repository.Event{event}
	if err := s.attachExdates(ctx, events); err != nil {
		return repository.Event{}, err
	}
	return events[0], nil
}

func (s *EventService) Update(ctx context.Context, userID int64, id, calendarID, title string, start, end time.Time, allDay bool, rrule string) (repository.Event, error) {
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

	updated, err := s.events.Update(ctx, userID, id, calendarID, title, start, end, allDay, rrule)
	if err != nil {
		return repository.Event{}, err
	}

	// A Master's rule changing (or being removed) is forced to "All events" and
	// discards existing Overrides/Exceptions, because their recurrence ids may
	// no longer be generated by the new rule (ADR-0016). The frontend warns
	// before calling this; a non-recurring Master or an unchanged rule has
	// nothing to discard.
	if existing.ParentID == nil && !samePattern(existing.Rrule, rrule) {
		if err := s.events.DeleteChildrenOf(ctx, userID, id); err != nil {
			return repository.Event{}, fmt.Errorf("discard overrides: %w", err)
		}
		if err := s.exceptions.DeleteByParentID(ctx, id); err != nil {
			return repository.Event{}, fmt.Errorf("discard exceptions: %w", err)
		}
	}

	events := []repository.Event{updated}
	if err := s.attachExdates(ctx, events); err != nil {
		return repository.Event{}, err
	}
	return events[0], nil
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

	if err := s.events.ReparentOverridesFrom(ctx, userID, oldParentID, newParentID, fromStart); err != nil {
		return fmt.Errorf("reparent overrides: %w", err)
	}
	if err := s.exceptions.ReparentFrom(ctx, oldParentID, newParentID, fromStart); err != nil {
		return fmt.Errorf("reparent exceptions: %w", err)
	}
	return nil
}
