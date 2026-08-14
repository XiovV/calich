// Package caldavserver implements emersion/go-webdav's caldav.Backend
// interface (ADR-0023) over this app's existing CalendarService and
// EventService, so a native calendar client can discover a User's Calendars
// as CalDAV collections and read their Events as calendar objects (#64).
//
// go-webdav's Calendar type has no field for the Apple/DAVx⁵ calendar-color
// extension and the library gives Backend no hook to inject extra PROPFIND
// XML, so exposing/accepting it is done by XML-patching outside the library
// (color.go, propfind.go, proppatch.go — ADR-0028).
package caldavserver

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/emersion/go-ical"
	"github.com/emersion/go-webdav"
	"github.com/emersion/go-webdav/caldav"
	"github.com/go-chi/chi/v5"

	"github.com/XiovV/calendar/server/internal/httpauth"
	"github.com/XiovV/calendar/server/internal/icalendar"
	"github.com/XiovV/calendar/server/internal/recurrence"
	"github.com/XiovV/calendar/server/internal/repository"
	"github.com/XiovV/calendar/server/internal/service"
)

const pathPrefix = "/dav"

// attachmentsBasePath is the RFC 8607 managed-attachments server URL,
// advertised path-only on the calendar home collection (managed-attachments-server-URL, propfind.go) and
// the base every managed-attachment URI ATTACH emits is built from
// (icalendar.CalDAVTarget's uriPrefix) — also this app's own
// download route (attachment_actions.go's GET handler), so the two always
// agree by construction (#133, ADR-0040).
const attachmentsBasePath = pathPrefix + "/attachments/"

// attachmentDownloadPath is one Attachment's full managed-attachment path —
// the value the CalDAV POST actions' Location header returns.
func attachmentDownloadPath(id string) string {
	return attachmentsBasePath + id
}

// attachmentsURIPrefix returns attachmentsBasePath made absolute with the
// scheme+host string dispatchHandler derived from the request in ctx, so ATTACH's
// URI (plain iCalendar text, unlike a WebDAV href, with no request to
// resolve a bare path against once it's sitting in a client's calendar
// store — #142) is a fully-qualified URL a native CalDAV client can
// actually dereference. Falls back to the path-only form if ctx carries no
// request (e.g. a test calling the Backend directly).
func attachmentsURIPrefix(ctx context.Context) string {
	baseURL, ok := baseURLFromContext(ctx)
	if !ok {
		return attachmentsBasePath
	}
	return baseURL + attachmentsBasePath
}

// chi only routes its own fixed set of HTTP methods by default; CalDAV
// clients also send PROPFIND/REPORT/MKCOL/PROPPATCH, so those must be
// registered before any router mounts CalDAV routes (ADR-0023, ADR-0028).
func init() {
	chi.RegisterMethod("PROPFIND")
	chi.RegisterMethod("REPORT")
	chi.RegisterMethod("MKCOL")
	chi.RegisterMethod("PROPPATCH")
}

type Backend struct {
	calendars   *service.CalendarService
	events      *service.EventService
	attachments *service.AttachmentService
	// maxAttachmentSize and maxAttachmentsPerEvent are Attachments' limits
	// (#132, ADR-0040), advertised on the calendar collection as RFC 8607's
	// max-attachment-size/max-attachments-per-resource (propfind.go) and
	// enforced against an attachment-add/-update body (attachment_actions.go).
	maxAttachmentSize      int64
	maxAttachmentsPerEvent int
}

func NewBackend(calendars *service.CalendarService, events *service.EventService, attachments *service.AttachmentService, maxAttachmentSize int64, maxAttachmentsPerEvent int) *Backend {
	return &Backend{calendars: calendars, events: events, attachments: attachments, maxAttachmentSize: maxAttachmentSize, maxAttachmentsPerEvent: maxAttachmentsPerEvent}
}

// Path depths below are load-bearing: go-webdav's PROPFIND dispatch
// classifies a request purely by how many path segments follow Handler.Prefix
// (1 = principal, 2 = calendar-home-set, 3 = calendar, 4 = calendar object —
// see resourceTypeAtPath in its caldav/server.go). It does not parse path
// semantics, so principal and home-set must land at depths 1 and 2 exactly
// (ADR-0023).

func principalPath(userID int64) string {
	return fmt.Sprintf("%s/%d/", pathPrefix, userID)
}

func homeSetPath(userID int64) string {
	return fmt.Sprintf("%s/%d/calendars/", pathPrefix, userID)
}

func calendarPath(userID int64, calendarID string) string {
	return fmt.Sprintf("%s/%d/calendars/%s/", pathPrefix, userID, calendarID)
}

// attendeeCollectionID is the reserved, non-UUID collection id backing the
// synthetic "Invitations" collection a principal's calendar home-set
// carries whenever they're an Attendee (ADR-0046) of at least one Event
// whose Calendar they have no Access to. Unlike every other entry
// ListCalendars returns, no real repository.Calendar row backs this
// collection for this principal — it exists purely to address those
// Attendee-only Events over CalDAV (#163). Real Calendar ids are
// uuid.New().String() values, which never collide with this fixed word.
const attendeeCollectionID = "attendee"

// attendeeCollectionName is the Invitations collection's displayname.
const attendeeCollectionName = "Invitations"

func attendeeCollectionPath(userID int64) string {
	return calendarPath(userID, attendeeCollectionID)
}

// errInvitationsReadOnly is PutCalendarObject and DeleteCalendarObject's
// shared refusal for the synthetic Invitations collection (#163): there is
// no ATTENDEE/PARTSTAT round-trip in the icalendar codec yet, and ADR-0046
// excludes the full iTIP/iMIP wire protocol outright, so this collection
// never accepts a write.
var errInvitationsReadOnly = webdav.NewHTTPError(http.StatusForbidden, fmt.Errorf("the Invitations collection is read-only"))

// calendarIDFromPath extracts {calendarId} from a collection path under
// userID's calendar-home-set, e.g. "/dav/1/calendars/abc-uuid/" -> "abc-uuid".
func calendarIDFromPath(userID int64, path string) (string, error) {
	prefix := homeSetPath(userID)
	if !strings.HasPrefix(path, prefix) {
		return "", fmt.Errorf("path %q is not under calendar home %q", path, prefix)
	}

	id := strings.Trim(strings.TrimPrefix(path, prefix), "/")
	if id == "" {
		return "", fmt.Errorf("no calendar id in path %q", path)
	}
	return id, nil
}

// calendarObjectPath returns a series' resource path: {masterId}.ics under
// its calendar's collection (ADR-0025).
func calendarObjectPath(userID int64, calendarID, masterID string) string {
	return fmt.Sprintf("%s%s.ics", calendarPath(userID, calendarID), masterID)
}

// calendarObjectIDFromPath extracts {calendarId} and {masterId} from an
// object path under userID's calendar home, e.g.
// "/dav/1/calendars/abc/def.ics" -> ("abc", "def").
func calendarObjectIDFromPath(userID int64, path string) (calendarID, masterID string, err error) {
	prefix := homeSetPath(userID)
	if !strings.HasPrefix(path, prefix) {
		return "", "", fmt.Errorf("path %q is not under calendar home %q", path, prefix)
	}

	rest := strings.TrimPrefix(path, prefix)
	parts := strings.SplitN(rest, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("no calendar object in path %q", path)
	}

	const suffix = ".ics"
	if !strings.HasSuffix(parts[1], suffix) {
		return "", "", fmt.Errorf("calendar object path %q does not end in %q", path, suffix)
	}

	return parts[0], strings.TrimSuffix(parts[1], suffix), nil
}

func toCalDAVCalendar(userID int64, c repository.Calendar) caldav.Calendar {
	return caldav.Calendar{
		Path: calendarPath(userID, c.ID),
		Name: c.Name,
	}
}

func userIDFromContext(ctx context.Context) (int64, error) {
	userID, ok := httpauth.UserIDFromContext(ctx)
	if !ok {
		return 0, fmt.Errorf("no authenticated user in context")
	}
	return userID, nil
}

func (b *Backend) CurrentUserPrincipal(ctx context.Context) (string, error) {
	userID, err := userIDFromContext(ctx)
	if err != nil {
		return "", err
	}
	return principalPath(userID), nil
}

func (b *Backend) CalendarHomeSetPath(ctx context.Context) (string, error) {
	userID, err := userIDFromContext(ctx)
	if err != nil {
		return "", err
	}
	return homeSetPath(userID), nil
}

func (b *Backend) ListCalendars(ctx context.Context) ([]caldav.Calendar, error) {
	userID, err := userIDFromContext(ctx)
	if err != nil {
		return nil, err
	}

	calendars, err := b.calendars.ListAccessible(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list calendars: %w", err)
	}

	result := make([]caldav.Calendar, len(calendars))
	for i, c := range calendars {
		result[i] = toCalDAVCalendar(userID, c.Calendar)
	}

	// The Invitations collection only appears once userID actually has an
	// Attendee-only Event to show — an absent one keeps the home-set
	// unchanged for every principal with no such invite (ADR-0046, #163).
	attendeeMasters, _, err := b.events.ListAttendeeOnlySeries(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list attendee-only events: %w", err)
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

	exists, currentETag, err := b.currentObjectETag(ctx, userID, calendarID, masterID)
	if err != nil {
		return nil, err
	}
	if opts != nil {
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
