package caldavserver

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/emersion/go-webdav"
	"github.com/emersion/go-webdav/caldav"
	"golang.org/x/sync/errgroup"

	"github.com/XiovV/calich/server/internal/repository"
	"github.com/XiovV/calich/server/internal/service"
)

func toCalDAVCalendar(userID int64, c repository.Calendar) caldav.Calendar {
	return caldav.Calendar{
		Path: calendarPath(userID, c.ID),
		Name: c.Name,
	}
}

func (b *Backend) ListCalendars(ctx context.Context) ([]caldav.Calendar, error) {
	userID, err := userIDFromContext(ctx)
	if err != nil {
		return nil, err
	}

	// ListAccessible and ListAttendeeOnlySeries don't depend on each
	// other's results, so they run concurrently via errgroup (#273).
	var calendars []service.CalendarWithAccess
	var attendeeMasters []repository.Event

	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		var err error
		calendars, err = b.calendars.ListAccessible(gctx, userID)
		if err != nil {
			return fmt.Errorf("list calendars: %w", err)
		}
		return nil
	})
	g.Go(func() error {
		var err error
		// The Invitations collection only appears once userID actually has
		// an Attendee-only Event to show — an absent one keeps the
		// home-set unchanged for every principal with no such invite
		// (ADR-0046, #163).
		attendeeMasters, _, err = b.events.ListAttendeeOnlySeries(gctx, userID)
		if err != nil {
			return fmt.Errorf("list attendee-only events: %w", err)
		}
		return nil
	})
	if err := g.Wait(); err != nil {
		return nil, err
	}

	result := make([]caldav.Calendar, len(calendars))
	for i, c := range calendars {
		result[i] = toCalDAVCalendar(userID, c.Calendar)
	}

	if len(attendeeMasters) > 0 {
		result = append(result, caldav.Calendar{Path: attendeeCollectionPath(userID), Name: attendeeCollectionName})
	}

	return result, nil
}

func (b *Backend) GetCalendar(ctx context.Context, path string) (*caldav.Calendar, error) {
	userID, err := userIDFromContext(ctx)
	if err != nil {
		return nil, err
	}

	calendarID, err := calendarIDFromPath(userID, path)
	if err != nil {
		return nil, webdav.NewHTTPError(http.StatusNotFound, err)
	}

	if calendarID == attendeeCollectionID {
		// Mirrors ListCalendars' own condition — the Invitations collection
		// resolves only while userID actually has an Attendee-only Event to
		// show, so a stale or guessed URL to it 404s once it has none, the
		// same as it never appeared in their home-set to begin with.
		attendeeMasters, _, err := b.events.ListAttendeeOnlySeries(ctx, userID)
		if err != nil {
			return nil, fmt.Errorf("list attendee-only events: %w", err)
		}
		if len(attendeeMasters) == 0 {
			return nil, webdav.NewHTTPError(http.StatusNotFound, fmt.Errorf("no attendee-only events"))
		}
		return &caldav.Calendar{Path: attendeeCollectionPath(userID), Name: attendeeCollectionName}, nil
	}

	c, err := b.calendars.Get(ctx, userID, calendarID)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, webdav.NewHTTPError(http.StatusNotFound, err)
	}
	if err != nil {
		return nil, fmt.Errorf("get calendar: %w", err)
	}

	result := toCalDAVCalendar(userID, c)
	return &result, nil
}

// CreateCalendar is not supported over CalDAV — Calendars are created from
// the web app only, at least for now.
func (b *Backend) CreateCalendar(ctx context.Context, calendar *caldav.Calendar) error {
	return webdav.NewHTTPError(http.StatusNotImplemented, fmt.Errorf("creating calendars over CalDAV is not supported"))
}
