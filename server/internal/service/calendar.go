package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/XiovV/calendar/server/internal/repository"
)

var (
	ErrInvalidColor = errors.New("invalid calendar color")
	ErrInvalidName  = errors.New("calendar name must not be empty")
)

type CalendarService struct {
	calendars *repository.CalendarRepository
}

func NewCalendarService(calendars *repository.CalendarRepository) *CalendarService {
	return &CalendarService{calendars: calendars}
}

func (s *CalendarService) Create(ctx context.Context, userID int64, id, name, color string) (repository.Calendar, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return repository.Calendar{}, ErrInvalidName
	}
	color, ok := NormalizeColor(color)
	if !ok {
		return repository.Calendar{}, ErrInvalidColor
	}

	calendar, err := s.calendars.Create(ctx, userID, id, name, color)
	if err != nil {
		return repository.Calendar{}, fmt.Errorf("create calendar: %w", err)
	}
	return calendar, nil
}

func (s *CalendarService) List(ctx context.Context, userID int64) ([]repository.Calendar, error) {
	calendars, err := s.calendars.ListByUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list calendars: %w", err)
	}
	return calendars, nil
}

func (s *CalendarService) Get(ctx context.Context, userID int64, id string) (repository.Calendar, error) {
	return s.calendars.GetByID(ctx, userID, id)
}

func (s *CalendarService) Update(ctx context.Context, userID int64, id, name, color string) (repository.Calendar, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return repository.Calendar{}, ErrInvalidName
	}
	color, ok := NormalizeColor(color)
	if !ok {
		return repository.Calendar{}, ErrInvalidColor
	}

	return s.calendars.Update(ctx, userID, id, name, color)
}

func (s *CalendarService) Delete(ctx context.Context, userID int64, id string) error {
	return s.calendars.Delete(ctx, userID, id)
}

type defaultCalendar struct {
	name  string
	color string
}

// These seed hexes are independent of the frontend's Swatch hexes
// (calendarColors.ts) — the two lists are allowed to drift (ADR-0029).
var defaultCalendars = []defaultCalendar{
	{name: "Personal", color: "#12809CFF"}, // peacock
	{name: "Work", color: "#E2483DFF"},     // tomato
	{name: "Family", color: "#6B9071FF"},   // sage
}

// EnsureDefaults seeds a user with the default Personal/Work/Family
// calendars if they don't have any calendars yet. It's a no-op once the user
// has at least one calendar, so it's safe to call on every startup rather
// than only on a freshly created user — though callers that only want to
// seed a genuinely fresh install should still gate the call on that signal
// (see AuthService.Bootstrap's created return value), since a user who
// deletes all their calendars would otherwise have them silently resurrected.
func (s *CalendarService) EnsureDefaults(ctx context.Context, userID int64) error {
	existing, err := s.List(ctx, userID)
	if err != nil {
		return fmt.Errorf("list calendars: %w", err)
	}
	if len(existing) > 0 {
		return nil
	}

	for _, d := range defaultCalendars {
		if _, err := s.Create(ctx, userID, uuid.NewString(), d.name, d.color); err != nil {
			return fmt.Errorf("create default calendar %q: %w", d.name, err)
		}
	}

	return nil
}
