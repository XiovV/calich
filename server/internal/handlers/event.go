package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/XiovV/calendar/server/internal/httpauth"
	"github.com/XiovV/calendar/server/internal/httpresponse"
	"github.com/XiovV/calendar/server/internal/repository"
	"github.com/XiovV/calendar/server/internal/service"
)

type EventHandler struct {
	events *service.EventService
}

func NewEventHandler(events *service.EventService) *EventHandler {
	return &EventHandler{events: events}
}

// dateOnlyLayout is the wire format for an all-day Event's start/end: a
// timezone-free date, e.g. "2026-08-04" (ADR-0017). A timed Event's start/end
// use the default time.Time RFC3339 encoding instead.
const dateOnlyLayout = "2006-01-02"

// parseEventTime parses a wire start/end string, branching on allDay so an
// all-day date-only string is never run through timezone-aware RFC3339
// parsing (ADR-0017).
func parseEventTime(raw string, allDay bool) (time.Time, error) {
	if allDay {
		return time.ParseInLocation(dateOnlyLayout, raw, time.UTC)
	}
	return time.Parse(time.RFC3339, raw)
}

// formatEventTime is parseEventTime's inverse for wire responses. A timed
// Event is always emitted as a canonical UTC "…Z" instant — never the offset
// it happened to be parsed with — so the client can only ever render off the
// Viewer zone or the sibling tzid, never a stray wire offset (ADR-0019).
func formatEventTime(t time.Time, allDay bool) string {
	if allDay {
		return t.UTC().Format(dateOnlyLayout)
	}
	return t.UTC().Format(time.RFC3339Nano)
}

// reminderWire is a Reminder's wire shape (ADR-0020), shared by requests and
// responses — a Reminder has no other representation.
type reminderWire struct {
	OffsetMinutes int    `json:"offsetMinutes"`
	Channel       string `json:"channel"`
}

func toReminderWire(reminders []repository.Reminder) []reminderWire {
	if len(reminders) == 0 {
		return nil
	}
	wire := make([]reminderWire, len(reminders))
	for i, r := range reminders {
		wire[i] = reminderWire{OffsetMinutes: r.OffsetMinutes, Channel: r.Channel}
	}
	return wire
}

func fromReminderWire(wire []reminderWire) []repository.Reminder {
	if len(wire) == 0 {
		return nil
	}
	reminders := make([]repository.Reminder, len(wire))
	for i, w := range wire {
		reminders[i] = repository.Reminder{OffsetMinutes: w.OffsetMinutes, Channel: w.Channel}
	}
	return reminders
}

// eventResponse is an Event's wire shape. Start and End are strings so their
// format can branch on AllDay (ADR-0017); toEventResponse is the only thing
// that builds one, and does that formatting. Field order here is the emitted
// JSON's field order.
type eventResponse struct {
	ID         string `json:"id"`
	CalendarID string `json:"calendarId"`
	Title      string `json:"title"`
	Start      string `json:"start"`
	End        string `json:"end"`
	// AllDay flags this Event as occupying whole dates rather than a time
	// range — see ADR-0017 and CONTEXT.md's All-day Event.
	AllDay bool   `json:"allDay,omitempty"`
	Rrule  string `json:"rrule,omitempty"`
	// ParentID and RecurrenceID are present only on an Override — a standalone
	// Event that replaces one Occurrence of its parent's series (ADR-0016).
	ParentID     *string    `json:"parentId,omitempty"`
	RecurrenceID *time.Time `json:"recurrenceId,omitempty"`
	// Exdates lists a Master's cancelled Occurrence starts (Exceptions).
	// Always absent on an Override.
	Exdates []time.Time `json:"exdates,omitempty"`
	// Tzid is the Event's Anchor zone: a named IANA zone, "Etc/UTC" for an
	// absolute instant, or nil for a Floating Event. See ADR-0019.
	Tzid *string `json:"tzid,omitempty"`
	// Reminders is this Event's Reminders (ADR-0020). Absent/empty means no
	// Reminders.
	Reminders []reminderWire `json:"reminders,omitempty"`
	// Description and Location are free-text fields on an Event (#61).
	Description string `json:"description,omitempty"`
	Location    string `json:"location,omitempty"`
}

func toEventResponse(e repository.Event) eventResponse {
	return eventResponse{
		ID:           e.ID,
		CalendarID:   e.CalendarID,
		Title:        e.Title,
		Start:        formatEventTime(e.Start, e.AllDay),
		End:          formatEventTime(e.End, e.AllDay),
		AllDay:       e.AllDay,
		Rrule:        e.Rrule,
		ParentID:     e.ParentID,
		RecurrenceID: e.RecurrenceID,
		Exdates:      e.Exdates,
		Tzid:         e.Tzid,
		Reminders:    toReminderWire(e.Reminders),
		Description:  e.Description,
		Location:     e.Location,
	}
}

// eventWriteErrors is the rendering shared by the create and update paths,
// which validate the same fields the same way.
var eventWriteErrors = []errorCase{
	{service.ErrInvalidTitle, badRequest("title must not be empty")},
	{service.ErrInvalidTimeRange, badRequest("end must be after start")},
	{service.ErrInvalidRecurrenceRule, badRequest("recurrence rule is invalid")},
	{service.ErrInvalidReminderChannel, badRequest("reminder channel must be \"notification\" or \"email\"")},
	{service.ErrCalendarNotFound, badRequest("calendar not found")},
	{service.ErrCalendarReadOnly, forbidden("calendar is read-only")},
}

// On create, a missing parent is named as such — parentId is a body field the
// client chose. On the exception and reparent paths it is the URL's event, so
// those render it as a plain "event not found" 404 instead.
var createEventErrors = alsoHandling(eventWriteErrors,
	errorCase{service.ErrInvalidOverride, badRequest("an override must not have its own recurrence rule, and requires a recurrence id")},
	errorCase{service.ErrParentIsOverride, badRequest("parent event must be a master, not an override")},
	errorCase{service.ErrParentNotFound, badRequest("parent event not found")},
)

var updateEventErrors = alsoHandling(eventWriteErrors,
	errorCase{service.ErrInvalidOverride, badRequest("an override must not have its own recurrence rule")},
	errorCase{repository.ErrNotFound, notFound("event not found")},
)

var addExceptionErrors = []errorCase{
	{service.ErrParentNotFound, notFound("event not found")},
	{service.ErrParentIsOverride, badRequest("parent event must be a master, not an override")},
	{service.ErrParentNotRecurring, badRequest("parent event does not recur")},
	{service.ErrCalendarReadOnly, forbidden("calendar is read-only")},
}

var reparentErrors = []errorCase{
	{service.ErrParentNotFound, notFound("event not found")},
	{service.ErrCalendarReadOnly, forbidden("calendar is read-only")},
}

var eventNotFoundErrors = []errorCase{
	{repository.ErrNotFound, notFound("event not found")},
	{service.ErrCalendarReadOnly, forbidden("calendar is read-only")},
}

func (h *EventHandler) List(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpauth.UserIDFromContext(r.Context())
	if !ok {
		httpresponse.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}

	from, ok := parseOptionalTimeParam(w, r, "from")
	if !ok {
		return
	}
	to, ok := parseOptionalTimeParam(w, r, "to")
	if !ok {
		return
	}

	events, err := h.events.List(r.Context(), userID, from, to)
	if err != nil {
		httpresponse.Error(w, http.StatusInternalServerError, "internal_error", "failed to list events")
		return
	}

	response := make([]eventResponse, len(events))
	for i, e := range events {
		response[i] = toEventResponse(e)
	}

	httpresponse.JSON(w, http.StatusOK, response)
}

func parseOptionalTimeParam(w http.ResponseWriter, r *http.Request, name string) (*time.Time, bool) {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return nil, true
	}

	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		httpresponse.Error(w, http.StatusBadRequest, "invalid_request", name+" must be an RFC3339 timestamp")
		return nil, false
	}
	return &parsed, true
}

// createEventRequest is the POST /api/events body. Start and End are strings
// so their format can branch on AllDay (ADR-0017) — parseEventTimes turns them
// into instants once the body has decoded.
type createEventRequest struct {
	ID           string         `json:"id"`
	CalendarID   string         `json:"calendarId"`
	Title        string         `json:"title"`
	Start        string         `json:"start"`
	End          string         `json:"end"`
	AllDay       bool           `json:"allDay,omitempty"`
	Rrule        string         `json:"rrule"`
	ParentID     *string        `json:"parentId,omitempty"`
	RecurrenceID *time.Time     `json:"recurrenceId,omitempty"`
	Tzid         *string        `json:"tzid,omitempty"`
	Reminders    []reminderWire `json:"reminders,omitempty"`
	Description  string         `json:"description,omitempty"`
	Location     string         `json:"location,omitempty"`
}

// parseEventTimes converts a decoded body's start/end strings into instants,
// branching on allDay (ADR-0017). Shared by create and update, which carry the
// same three fields.
func parseEventTimes(rawStart, rawEnd string, allDay bool) (start, end time.Time, err error) {
	start, err = parseEventTime(rawStart, allDay)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("invalid start: %w", err)
	}
	end, err = parseEventTime(rawEnd, allDay)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("invalid end: %w", err)
	}
	return start, end, nil
}

func (h *EventHandler) Create(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpauth.UserIDFromContext(r.Context())
	if !ok {
		httpresponse.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}

	var req createEventRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpresponse.Error(w, http.StatusBadRequest, "invalid_request", "request body must be valid JSON")
		return
	}

	// Checked before the id, matching the order when start/end were parsed
	// inside UnmarshalJSON — i.e. during Decode, above.
	start, end, err := parseEventTimes(req.Start, req.End, req.AllDay)
	if err != nil {
		httpresponse.Error(w, http.StatusBadRequest, "invalid_request", "request body must be valid JSON")
		return
	}

	if _, err := uuid.Parse(req.ID); err != nil {
		httpresponse.Error(w, http.StatusBadRequest, "invalid_request", "id must be a valid UUID")
		return
	}

	event, err := h.events.Create(r.Context(), userID, req.ID, service.EventWrite{
		CalendarID:   req.CalendarID,
		Title:        req.Title,
		Start:        start,
		End:          end,
		AllDay:       req.AllDay,
		Rrule:        req.Rrule,
		ParentID:     req.ParentID,
		RecurrenceID: req.RecurrenceID,
		Tzid:         req.Tzid,
		Reminders:    fromReminderWire(req.Reminders),
		Description:  req.Description,
		Location:     req.Location,
	})
	if respondError(w, err, createEventErrors, "failed to create event") {
		return
	}

	httpresponse.JSON(w, http.StatusCreated, toEventResponse(event))
}

func (h *EventHandler) Get(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpauth.UserIDFromContext(r.Context())
	if !ok {
		httpresponse.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}

	id := chi.URLParam(r, "id")

	event, err := h.events.Get(r.Context(), userID, id)
	if respondError(w, err, eventNotFoundErrors, "failed to load event") {
		return
	}

	httpresponse.JSON(w, http.StatusOK, toEventResponse(event))
}

// updateEventRequest is the PATCH /api/events/{id} body. Start and End are
// strings for the same reason as createEventRequest's (ADR-0017).
type updateEventRequest struct {
	CalendarID  string         `json:"calendarId"`
	Title       string         `json:"title"`
	Start       string         `json:"start"`
	End         string         `json:"end"`
	AllDay      bool           `json:"allDay,omitempty"`
	Rrule       string         `json:"rrule"`
	Tzid        *string        `json:"tzid,omitempty"`
	Reminders   []reminderWire `json:"reminders,omitempty"`
	Description string         `json:"description,omitempty"`
	Location    string         `json:"location,omitempty"`
}

func (h *EventHandler) Update(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpauth.UserIDFromContext(r.Context())
	if !ok {
		httpresponse.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}

	id := chi.URLParam(r, "id")

	var req updateEventRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpresponse.Error(w, http.StatusBadRequest, "invalid_request", "request body must be valid JSON")
		return
	}

	start, end, err := parseEventTimes(req.Start, req.End, req.AllDay)
	if err != nil {
		httpresponse.Error(w, http.StatusBadRequest, "invalid_request", "request body must be valid JSON")
		return
	}

	event, err := h.events.Update(r.Context(), userID, id, service.EventWrite{
		CalendarID:  req.CalendarID,
		Title:       req.Title,
		Start:       start,
		End:         end,
		AllDay:      req.AllDay,
		Rrule:       req.Rrule,
		Tzid:        req.Tzid,
		Reminders:   fromReminderWire(req.Reminders),
		Description: req.Description,
		Location:    req.Location,
	})
	if respondError(w, err, updateEventErrors, "failed to update event") {
		return
	}

	httpresponse.JSON(w, http.StatusOK, toEventResponse(event))
}

func (h *EventHandler) Delete(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpauth.UserIDFromContext(r.Context())
	if !ok {
		httpresponse.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}

	id := chi.URLParam(r, "id")

	err := h.events.Delete(r.Context(), userID, id)
	if respondError(w, err, eventNotFoundErrors, "failed to delete event") {
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

type createExceptionRequest struct {
	OccurrenceStart time.Time `json:"occurrenceStart"`
}

// AddException cancels a single Occurrence of a recurring master (deleting
// "this event" on a recurring Occurrence), storing it as an iCalendar EXDATE
// (ADR-0016).
func (h *EventHandler) AddException(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpauth.UserIDFromContext(r.Context())
	if !ok {
		httpresponse.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}

	id := chi.URLParam(r, "id")

	var req createExceptionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpresponse.Error(w, http.StatusBadRequest, "invalid_request", "request body must be valid JSON")
		return
	}

	err := h.events.AddException(r.Context(), userID, id, req.OccurrenceStart)
	if respondError(w, err, addExceptionErrors, "failed to add exception") {
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

type reparentRequest struct {
	NewParentID string    `json:"newParentId"`
	FromStart   time.Time `json:"fromStart"`
}

// Reparent moves every Override/Exception of the named event at-or-after
// fromStart to belong to newParentId instead — the "this and following" split
// reparenting overrides/exceptions at the boundary (ADR-0016).
func (h *EventHandler) Reparent(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpauth.UserIDFromContext(r.Context())
	if !ok {
		httpresponse.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}

	id := chi.URLParam(r, "id")

	var req reparentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpresponse.Error(w, http.StatusBadRequest, "invalid_request", "request body must be valid JSON")
		return
	}

	err := h.events.ReparentFrom(r.Context(), userID, id, req.NewParentID, req.FromStart)
	if respondError(w, err, reparentErrors, "failed to reparent series") {
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
