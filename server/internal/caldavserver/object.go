package caldavserver

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/emersion/go-ical"
	"github.com/emersion/go-webdav"
	"github.com/emersion/go-webdav/caldav"

	"github.com/XiovV/calich/server/internal/icalendar"
	"github.com/XiovV/calich/server/internal/repository"
	"github.com/XiovV/calich/server/internal/service"
)

// errInvitationsReadOnly is PutCalendarObject and DeleteCalendarObject's
// shared refusal for the synthetic Invitations collection (#163): there is
// no ATTENDEE/PARTSTAT round-trip in the icalendar codec yet, and ADR-0046
// excludes the full iTIP/iMIP wire protocol outright, so this collection
// never accepts a write.
var errInvitationsReadOnly = webdav.NewHTTPError(http.StatusForbidden, fmt.Errorf("the Invitations collection is read-only"))

// PutCalendarObject decomposes calendar into masterID's Master, Overrides,
// and Exdates and writes them atomically (ADR-0025, #66): masterID is
// created if new, updated in place if it already exists. opts' conditional
// headers are checked against the object's current ETag (recomputed from
// its live reconstruction, never a raw stored value — ADR-0026) before any
// write happens, so a stale If-Match is rejected without touching data.
func (b *Backend) PutCalendarObject(ctx context.Context, path string, calendar *ical.Calendar, opts *caldav.PutCalendarObjectOptions) (*caldav.CalendarObject, error) {
	userID, err := userIDFromContext(ctx)
	if err != nil {
		return nil, err
	}

	calendarID, masterID, err := calendarObjectIDFromPath(userID, path)
	if err != nil {
		return nil, webdav.NewHTTPError(http.StatusNotFound, err)
	}
	if calendarID == attendeeCollectionID {
		return nil, errInvitationsReadOnly
	}

	parsed, err := icalendar.ParseCalendarObject(calendar)
	if err != nil {
		return nil, webdav.NewHTTPError(http.StatusBadRequest, err)
	}
	if parsed.UID != masterID {
		return nil, webdav.NewHTTPError(http.StatusConflict, fmt.Errorf("VEVENT UID %q does not match resource name %q", parsed.UID, masterID))
	}

	// Recomputing the current ETag means fully reconstructing the series
	// (GetSeries, re-encode, SHA256) — wasted work when opts carries no
	// precondition to check it against (#273).
	if opts != nil && (opts.IfMatch.IsSet() || opts.IfNoneMatch.IsSet()) {
		exists, currentETag, err := b.currentObjectETag(ctx, userID, calendarID, masterID)
		if err != nil {
			return nil, err
		}
		if err := checkPutPreconditions(*opts, exists, currentETag); err != nil {
			return nil, err
		}
	}

	master, overrides, err := b.events.PutSeries(ctx, userID, calendarID, masterID, seriesWriteFromParsed(parsed))
	if err != nil {
		return nil, mapPutSeriesError(err)
	}

	return buildCalendarObject(ctx, userID, calendarID, master, overrides)
}

// checkPutPreconditions enforces opts' RFC 2068 conditional headers against
// the object's current state: If-None-Match: * rejects overwriting an
// object that already exists; If-Match rejects a stale ETag (or a PUT to an
// object that doesn't exist yet, since there is nothing for it to match).
func checkPutPreconditions(opts caldav.PutCalendarObjectOptions, exists bool, currentETag string) error {
	if opts.IfNoneMatch.IsSet() && opts.IfNoneMatch.IsWildcard() && exists {
		return webdav.NewHTTPError(http.StatusPreconditionFailed, fmt.Errorf("calendar object already exists"))
	}
	if opts.IfMatch.IsSet() {
		if !exists {
			return webdav.NewHTTPError(http.StatusPreconditionFailed, fmt.Errorf("calendar object does not exist"))
		}
		return checkIfMatch(opts.IfMatch, currentETag)
	}
	return nil
}

// checkIfMatch rejects a PUT/DELETE whose If-Match header doesn't match
// currentETag — the precondition check PutCalendarObject and
// DeleteCalendarObject both apply once they've confirmed the object exists.
func checkIfMatch(ifMatch webdav.ConditionalMatch, currentETag string) error {
	ok, err := ifMatch.MatchETag(currentETag)
	if err != nil {
		return webdav.NewHTTPError(http.StatusBadRequest, err)
	}
	if !ok {
		return webdav.NewHTTPError(http.StatusPreconditionFailed, fmt.Errorf("if-match precondition failed"))
	}
	return nil
}

// currentObjectETag reports whether masterID currently resolves under
// calendarID and, if so, its current ETag — computed the same way
// GetCalendarObject computes one, so an If-Match check compares against
// exactly what the next GET would have returned (ADR-0026).
func (b *Backend) currentObjectETag(ctx context.Context, userID int64, calendarID, masterID string) (exists bool, etag string, err error) {
	master, overrides, err := b.events.GetSeries(ctx, userID, masterID)
	if errors.Is(err, repository.ErrNotFound) || errors.Is(err, service.ErrParentIsOverride) {
		return false, "", nil
	}
	if err != nil {
		return false, "", fmt.Errorf("get series: %w", err)
	}
	if master.CalendarID != calendarID {
		return false, "", nil
	}

	cal, _, err := icalendar.SeriesToICal(master, overrides, icalendar.CalDAVTarget(attachmentsURIPrefix(ctx)))
	if err != nil {
		return false, "", err
	}
	etag, err = icalendar.CalendarETag(cal)
	if err != nil {
		return false, "", err
	}
	return true, etag, nil
}

// seriesWriteFromParsed converts a decomposed calendar object into the
// service.SeriesWrite PutSeries writes.
func seriesWriteFromParsed(parsed *icalendar.ParsedSeries) service.SeriesWrite {
	overrides := make([]service.OverrideWrite, len(parsed.Overrides))
	for i, o := range parsed.Overrides {
		overrides[i] = service.OverrideWrite{
			RecurrenceID: o.RecurrenceID,
			Title:        o.Title,
			Description:  o.Description,
			Location:     o.Location,
			URL:          o.URL,
			Start:        o.Start,
			End:          o.End,
			AllDay:       o.AllDay,
			Tzid:         o.Tzid,
			Reminders:    o.Reminders,
			Color:        o.Color,
		}
	}

	return service.SeriesWrite{
		Title:       parsed.Master.Title,
		Description: parsed.Master.Description,
		Location:    parsed.Master.Location,
		URL:         parsed.Master.URL,
		Start:       parsed.Master.Start,
		End:         parsed.Master.End,
		AllDay:      parsed.Master.AllDay,
		Tzid:        parsed.Master.Tzid,
		Rrule:       parsed.Rrule,
		Reminders:   parsed.Master.Reminders,
		Exdates:     parsed.Exdates,
		Overrides:   overrides,
		Color:       parsed.Master.Color,
	}
}

// mapPutSeriesError maps PutSeries' validation/lookup errors onto the HTTP
// status a CalDAV client expects: a bad request body is 400, an unresolved
// calendar or an id naming an Override is 404 (matching GetCalendarObject),
// and a Subscribed Calendar's collection is 403 (ADR-0032) — it exists and
// is visible, the write is simply refused.
func mapPutSeriesError(err error) error {
	switch {
	case errors.Is(err, service.ErrCalendarReadOnly):
		return webdav.NewHTTPError(http.StatusForbidden, err)
	case errors.Is(err, service.ErrCalendarNotFound), errors.Is(err, service.ErrParentIsOverride), errors.Is(err, repository.ErrNotFound):
		return webdav.NewHTTPError(http.StatusNotFound, err)
	case errors.Is(err, service.ErrInvalidTitle),
		errors.Is(err, service.ErrInvalidTimeRange),
		errors.Is(err, service.ErrInvalidRecurrenceRule),
		errors.Is(err, service.ErrInvalidReminderChannel),
		errors.Is(err, service.ErrInvalidEventColor):
		return webdav.NewHTTPError(http.StatusBadRequest, err)
	default:
		return fmt.Errorf("put calendar object: %w", err)
	}
}

// DeleteCalendarObject deletes masterID's whole series — the Master row
// plus its Overrides and Exceptions, cascaded by the database (ADR-0018) —
// and records a deleted_objects tombstone so a later sync-collection REPORT
// reports the removal (ADR-0025, #67). If-Match is honored via the header
// dispatchHandler stashes into ctx for DELETE (go-webdav's
// DeleteCalendarObject signature carries no options, unlike
// PutCalendarObject's). A Subscribed Calendar's object refuses the delete
// with 403, not 404 — the object is visible, the write is refused
// (ADR-0032).
func (b *Backend) DeleteCalendarObject(ctx context.Context, path string) error {
	userID, err := userIDFromContext(ctx)
	if err != nil {
		return err
	}

	calendarID, masterID, err := calendarObjectIDFromPath(userID, path)
	if err != nil {
		return webdav.NewHTTPError(http.StatusNotFound, err)
	}
	if calendarID == attendeeCollectionID {
		return errInvitationsReadOnly
	}

	exists, currentETag, err := b.currentObjectETag(ctx, userID, calendarID, masterID)
	if err != nil {
		return err
	}
	if !exists {
		return webdav.NewHTTPError(http.StatusNotFound, fmt.Errorf("calendar object not found"))
	}

	if ifMatch, ok := ifMatchFromContext(ctx); ok {
		if err := checkIfMatch(webdav.ConditionalMatch(ifMatch), currentETag); err != nil {
			return err
		}
	}

	if err := b.events.Delete(ctx, userID, masterID); err != nil {
		if errors.Is(err, service.ErrCalendarReadOnly) {
			return webdav.NewHTTPError(http.StatusForbidden, err)
		}
		return fmt.Errorf("delete calendar object: %w", err)
	}
	return nil
}
