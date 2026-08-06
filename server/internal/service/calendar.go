package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

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

// CalendarWrite is a Calendar's writable fields, gathered into one value the
// same way EventWrite already gathers an event's — so Create and Update take
// one argument each instead of separately threading every field.
type CalendarWrite struct {
	Name  string
	Color string
	// SourceURL is non-nil only when Create is subscribing to an external
	// feed (#83, ADR-0032) — never set by the plain create/update paths.
	SourceURL *string
	// KeepAlarms is set only alongside SourceURL, at Subscribe time (#87,
	// ADR-0032) — a later change goes through UpdateKeepAlarms instead,
	// since Update ignores this field just like SourceURL.
	KeepAlarms bool
}

// fields projects the write onto the columns the repository stores.
func (w CalendarWrite) fields() repository.CalendarFields {
	return repository.CalendarFields{
		Name:       w.Name,
		Color:      w.Color,
		SourceURL:  w.SourceURL,
		KeepAlarms: w.KeepAlarms,
	}
}

func (s *CalendarService) Create(ctx context.Context, userID int64, id string, write CalendarWrite) (repository.Calendar, error) {
	write.Name = strings.TrimSpace(write.Name)
	if write.Name == "" {
		return repository.Calendar{}, ErrInvalidName
	}
	color, ok := NormalizeColor(write.Color)
	if !ok {
		return repository.Calendar{}, ErrInvalidColor
	}
	write.Color = color

	calendar, err := s.calendars.Create(ctx, userID, id, write.fields())
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

func (s *CalendarService) Update(ctx context.Context, userID int64, id string, write CalendarWrite) (repository.Calendar, error) {
	write.Name = strings.TrimSpace(write.Name)
	if write.Name == "" {
		return repository.Calendar{}, ErrInvalidName
	}
	color, ok := NormalizeColor(write.Color)
	if !ok {
		return repository.Calendar{}, ErrInvalidColor
	}
	write.Color = color

	return s.calendars.Update(ctx, userID, id, write.fields())
}

func (s *CalendarService) Delete(ctx context.Context, userID int64, id string) error {
	return s.calendars.Delete(ctx, userID, id)
}

// RecordRefreshSuccess records a successful Refresh's outcome on id's
// Calendar (#85, #86, ADR-0033): the response validators (or content hash)
// to send back on the next conditional GET, when it completed, when the
// poller should attempt it next, and the publisher's stated cadence if
// observed. Always resets the failure/backoff state.
func (s *CalendarService) RecordRefreshSuccess(ctx context.Context, userID int64, id string, success repository.RefreshSuccess) error {
	return s.calendars.RecordRefreshSuccess(ctx, userID, id, success)
}

// RecordRefreshFailure records a failed Refresh attempt on id's Calendar
// (#86, ADR-0033): the classified reason, the new consecutive-failure count,
// and when to retry. Never disables or deletes the Calendar.
func (s *CalendarService) RecordRefreshFailure(ctx context.Context, userID int64, id string, failure repository.RefreshFailure) error {
	return s.calendars.RecordRefreshFailure(ctx, userID, id, failure)
}

// ScheduleNextRefresh sets a brand new Subscription's first due time (#86,
// ADR-0033), before any Refresh has run against it.
func (s *CalendarService) ScheduleNextRefresh(ctx context.Context, userID int64, id string, nextRefreshAt time.Time) error {
	return s.calendars.ScheduleNextRefresh(ctx, userID, id, nextRefreshAt)
}

// UpdateKeepAlarms changes id's keep_alarms setting alone (#87, ADR-0032).
// SubscribeService.UpdateKeepAlarms is the caller that actually enforces
// the Subscribed-Calendar-only rule and cascades the reminder cleanup a
// turn-off requires; this method is the plain column write underneath it.
func (s *CalendarService) UpdateKeepAlarms(ctx context.Context, userID int64, id string, keepAlarms bool) (repository.Calendar, error) {
	return s.calendars.UpdateKeepAlarms(ctx, userID, id, keepAlarms)
}

// ListDueForRefresh returns every Subscribed Calendar, across every user,
// whose next_refresh_at has come due — the background poller's read path
// (#86, ADR-0033).
func (s *CalendarService) ListDueForRefresh(ctx context.Context, now time.Time) ([]repository.Calendar, error) {
	return s.calendars.ListDueForRefresh(ctx, now)
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
		if _, err := s.Create(ctx, userID, uuid.NewString(), CalendarWrite{Name: d.name, Color: d.color}); err != nil {
			return fmt.Errorf("create default calendar %q: %w", d.name, err)
		}
	}

	return nil
}
