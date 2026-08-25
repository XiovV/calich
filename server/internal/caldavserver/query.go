package caldavserver

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/emersion/go-ical"
	"github.com/emersion/go-webdav"
	"github.com/emersion/go-webdav/caldav"

	"github.com/XiovV/calich/server/internal/icalendar"
	"github.com/XiovV/calich/server/internal/recurrence"
	"github.com/XiovV/calich/server/internal/repository"
	"github.com/XiovV/calich/server/internal/service"
)

// listSeriesObjects builds one calendar object per Master Event in the
// Calendar at path, keeping only the series include accepts. A nil include
// keeps every series. Shared by the two read paths go-webdav dispatches to —
// PROPFIND's enumeration and calendar-query's filtered REPORT — which differ
// only in that filter (ADR-0025).
func (b *Backend) listSeriesObjects(ctx context.Context, path string, include func(master repository.Event) (bool, error)) ([]caldav.CalendarObject, error) {
	userID, err := userIDFromContext(ctx)
	if err != nil {
		return nil, err
	}

	calendarID, err := calendarIDFromPath(userID, path)
	if err != nil {
		return nil, webdav.NewHTTPError(http.StatusNotFound, err)
	}

	var masters []repository.Event
	var overridesByParent map[string][]repository.Event
	if calendarID == attendeeCollectionID {
		masters, overridesByParent, err = b.events.ListAttendeeOnlySeries(ctx, userID)
	} else {
		masters, overridesByParent, err = b.events.ListSeriesByCalendar(ctx, userID, calendarID)
	}
	if err != nil {
		return nil, fmt.Errorf("list series: %w", err)
	}

	if include == nil {
		include = func(repository.Event) (bool, error) { return true, nil }
	}

	objects := make([]caldav.CalendarObject, 0, len(masters))
	for _, master := range masters {
		keep, err := include(master)
		if err != nil {
			return nil, err
		}
		if !keep {
			continue
		}

		object, err := buildCalendarObject(ctx, userID, calendarID, master, overridesByParent[master.ID])
		if err != nil {
			return nil, err
		}
		objects = append(objects, *object)
	}
	return objects, nil
}

// ListCalendarObjects returns one calendar object per Master Event in the
// Calendar at path — every series in full (ADR-0025). Partial retrieval via
// req's component/property selection is not implemented; every object is
// returned in full.
func (b *Backend) ListCalendarObjects(ctx context.Context, path string, req *caldav.CalendarCompRequest) ([]caldav.CalendarObject, error) {
	return b.listSeriesObjects(ctx, path, nil)
}

// QueryCalendarObjects implements calendar-query: it returns every series in
// the Calendar at path that has an Occurrence starting in the query's
// time-range, expanded at query time via rrule-go (ADR-0025's #64 acceptance
// criteria) — stored objects are never expanded in the database. A query
// without a time-range behaves like ListCalendarObjects.
func (b *Backend) QueryCalendarObjects(ctx context.Context, path string, query *caldav.CalendarQuery) ([]caldav.CalendarObject, error) {
	from, to, hasRange := timeRangeFromQuery(query)
	if !hasRange {
		return b.listSeriesObjects(ctx, path, nil)
	}

	return b.listSeriesObjects(ctx, path, func(master repository.Event) (bool, error) {
		inRange, err := seriesHasOccurrenceInRange(master, from, to)
		if err != nil {
			return false, fmt.Errorf("expand series %q: %w", master.ID, err)
		}
		return inRange, nil
	})
}

// timeRangeFromQuery extracts the VEVENT-level time-range a calendar-query
// REPORT filters on, if any. A query with no time-range (a bare comp-filter)
// reports ok=false.
func timeRangeFromQuery(query *caldav.CalendarQuery) (from, to time.Time, ok bool) {
	for _, comp := range query.CompFilter.Comps {
		if comp.Name == ical.CompEvent && !comp.Start.IsZero() && !comp.End.IsZero() {
			return comp.Start, comp.End, true
		}
	}
	return time.Time{}, time.Time{}, false
}

// seriesHasOccurrenceInRange reports whether master's series — its rrule
// expansion minus its Exceptions, or its own start/end if it does not
// recur — has an Occurrence overlapping the half-open window [from, to).
// Overrides are not considered: a series is in range based on the pattern
// its Master's rrule generates, matching this issue's acceptance criteria.
func seriesHasOccurrenceInRange(master repository.Event, from, to time.Time) (bool, error) {
	duration := master.End.Sub(master.Start)

	if master.Rrule == "" {
		return master.Start.Before(to) && master.Start.Add(duration).After(from), nil
	}

	// Occurrences starting before "from" can still overlap it, so the
	// expansion window is padded on the left by the series' own duration.
	starts, err := recurrence.ExpandOccurrences(master.Rrule, master.Tzid, master.Start, from.Add(-duration), to)
	if err != nil {
		return false, err
	}

	excluded := make(map[int64]struct{}, len(master.Exdates))
	for _, exdate := range master.Exdates {
		excluded[exdate.UTC().UnixNano()] = struct{}{}
	}

	for _, start := range starts {
		if _, isExcluded := excluded[start.UnixNano()]; isExcluded {
			continue
		}
		if start.Before(to) && start.Add(duration).After(from) {
			return true, nil
		}
	}
	return false, nil
}

// buildCalendarObject recomposes master and its overrides into the
// CalendarObject GetCalendarObject/ListCalendarObjects/QueryCalendarObjects
// all serve (ADR-0025). collectionID names the collection the resulting
// object's Path is addressed under — master.CalendarID for every real
// Calendar collection, or attendeeCollectionID for the synthetic
// Invitations collection (#163), which never shares master.CalendarID
// since no per-principal collection backs a Calendar the caller lacks
// Access to.
func buildCalendarObject(ctx context.Context, userID int64, collectionID string, master repository.Event, overrides []repository.Event) (*caldav.CalendarObject, error) {
	cal, _, err := icalendar.SeriesToICal(master, overrides, icalendar.CalDAVTarget(attachmentsURIPrefix(ctx)))
	if err != nil {
		return nil, fmt.Errorf("serialize series %q: %w", master.ID, err)
	}

	etag, err := icalendar.CalendarETag(cal)
	if err != nil {
		return nil, fmt.Errorf("compute etag for series %q: %w", master.ID, err)
	}

	return &caldav.CalendarObject{
		Path:    calendarObjectPath(userID, collectionID, master.ID),
		ModTime: master.CreatedAt,
		ETag:    etag,
		Data:    cal,
	}, nil
}

// GetCalendarObject returns the series addressed by path — the Master named
// by {masterId}.ics plus its Overrides, recomposed into one VCALENDAR
// (ADR-0025). Not found if the id names an Override (never independently
// addressable) or a different Calendar's Event.
func (b *Backend) GetCalendarObject(ctx context.Context, path string, req *caldav.CalendarCompRequest) (*caldav.CalendarObject, error) {
	userID, err := userIDFromContext(ctx)
	if err != nil {
		return nil, err
	}

	calendarID, masterID, err := calendarObjectIDFromPath(userID, path)
	if err != nil {
		return nil, webdav.NewHTTPError(http.StatusNotFound, err)
	}

	if calendarID == attendeeCollectionID {
		master, overrides, err := b.events.GetAttendeeOnlySeries(ctx, userID, masterID)
		if errors.Is(err, repository.ErrNotFound) {
			return nil, webdav.NewHTTPError(http.StatusNotFound, fmt.Errorf("calendar object not found"))
		}
		if err != nil {
			return nil, fmt.Errorf("get attendee-only series: %w", err)
		}
		return buildCalendarObject(ctx, userID, calendarID, master, overrides)
	}

	master, overrides, err := b.events.GetSeries(ctx, userID, masterID)
	if errors.Is(err, repository.ErrNotFound) || errors.Is(err, service.ErrParentIsOverride) {
		return nil, webdav.NewHTTPError(http.StatusNotFound, fmt.Errorf("calendar object not found"))
	}
	if err != nil {
		return nil, fmt.Errorf("get series: %w", err)
	}
	if master.CalendarID != calendarID {
		return nil, webdav.NewHTTPError(http.StatusNotFound, fmt.Errorf("calendar object not found"))
	}

	return buildCalendarObject(ctx, userID, calendarID, master, overrides)
}
