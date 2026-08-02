package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/XiovV/calendar/server/internal/repository"
)

// CalendarColors are the only colors a calendar may be assigned, mirroring
// the frontend's CalendarColor union in calendarColors.ts.
var CalendarColors = []string{
	"tomato", "flamingo", "banana", "sage", "peacock", "blueberry", "grape", "graphite",
}

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

func (s *CalendarService) IsValidColor(color string) bool {
	for _, c := range CalendarColors {
		if c == color {
			return true
		}
	}
	return false
}

func (s *CalendarService) Create(ctx context.Context, userID int64, id, name, color string) (repository.Calendar, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return repository.Calendar{}, ErrInvalidName
	}
	if !s.IsValidColor(color) {
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
	if !s.IsValidColor(color) {
		return repository.Calendar{}, ErrInvalidColor
	}

	return s.calendars.Update(ctx, userID, id, name, color)
}

func (s *CalendarService) Delete(ctx context.Context, userID int64, id string) error {
	return s.calendars.Delete(ctx, userID, id)
}
