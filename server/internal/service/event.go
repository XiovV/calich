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
)

type EventService struct {
	events    *repository.EventRepository
	calendars *CalendarService
}

func NewEventService(events *repository.EventRepository, calendars *CalendarService) *EventService {
	return &EventService{events: events, calendars: calendars}
}

func (s *EventService) Create(ctx context.Context, userID int64, id, calendarID, title string, start, end time.Time, rrule string) (repository.Event, error) {
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

	event, err := s.events.Create(ctx, id, userID, calendarID, title, start, end, rrule)
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

func (s *EventService) List(ctx context.Context, userID int64, from, to *time.Time) ([]repository.Event, error) {
	events, err := s.events.ListByUser(ctx, userID, from, to)
	if err != nil {
		return nil, fmt.Errorf("list events: %w", err)
	}
	return events, nil
}

func (s *EventService) Get(ctx context.Context, userID int64, id string) (repository.Event, error) {
	return s.events.GetByID(ctx, userID, id)
}

func (s *EventService) Update(ctx context.Context, userID int64, id, calendarID, title string, start, end time.Time, rrule string) (repository.Event, error) {
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

	return s.events.Update(ctx, userID, id, calendarID, title, start, end, rrule)
}

func (s *EventService) Delete(ctx context.Context, userID int64, id string) error {
	return s.events.Delete(ctx, userID, id)
}
