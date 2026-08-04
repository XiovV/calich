package handlers

import (
	"encoding/json"
	"errors"
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

type eventResponse struct {
	ID         string    `json:"id"`
	CalendarID string    `json:"calendarId"`
	Title      string    `json:"title"`
	Start      time.Time `json:"-"`
	End        time.Time `json:"-"`
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
}

// eventResponseWire is eventResponse's actual JSON shape: start/end are
// strings so their format can branch on AllDay (ADR-0017).
type eventResponseWire struct {
	ID           string         `json:"id"`
	CalendarID   string         `json:"calendarId"`
	Title        string         `json:"title"`
	Start        string         `json:"start"`
	End          string         `json:"end"`
	AllDay       bool           `json:"allDay,omitempty"`
	Rrule        string         `json:"rrule,omitempty"`
	ParentID     *string        `json:"parentId,omitempty"`
	RecurrenceID *time.Time     `json:"recurrenceId,omitempty"`
	Exdates      []time.Time    `json:"exdates,omitempty"`
	Tzid         *string        `json:"tzid,omitempty"`
	Reminders    []reminderWire `json:"reminders,omitempty"`
}

func (r eventResponse) MarshalJSON() ([]byte, error) {
	return json.Marshal(eventResponseWire{
		ID:           r.ID,
		CalendarID:   r.CalendarID,
		Title:        r.Title,
		Start:        formatEventTime(r.Start, r.AllDay),
		End:          formatEventTime(r.End, r.AllDay),
		AllDay:       r.AllDay,
		Rrule:        r.Rrule,
		ParentID:     r.ParentID,
		RecurrenceID: r.RecurrenceID,
		Exdates:      r.Exdates,
		Tzid:         r.Tzid,
		Reminders:    r.Reminders,
	})
}

func (r *eventResponse) UnmarshalJSON(data []byte) error {
	var wire eventResponseWire
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	start, err := parseEventTime(wire.Start, wire.AllDay)
	if err != nil {
		return fmt.Errorf("invalid start: %w", err)
	}
	end, err := parseEventTime(wire.End, wire.AllDay)
	if err != nil {
		return fmt.Errorf("invalid end: %w", err)
	}
	*r = eventResponse{
		ID:           wire.ID,
		CalendarID:   wire.CalendarID,
		Title:        wire.Title,
		Start:        start,
		End:          end,
		AllDay:       wire.AllDay,
		Rrule:        wire.Rrule,
		ParentID:     wire.ParentID,
		RecurrenceID: wire.RecurrenceID,
		Exdates:      wire.Exdates,
		Tzid:         wire.Tzid,
		Reminders:    wire.Reminders,
	}
	return nil
}

func toEventResponse(e repository.Event) eventResponse {
	return eventResponse{
		ID:           e.ID,
		CalendarID:   e.CalendarID,
		Title:        e.Title,
		Start:        e.Start,
		End:          e.End,
		AllDay:       e.AllDay,
		Rrule:        e.Rrule,
		ParentID:     e.ParentID,
		RecurrenceID: e.RecurrenceID,
		Exdates:      e.Exdates,
		Tzid:         e.Tzid,
		Reminders:    toReminderWire(e.Reminders),
	}
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

type createEventRequest struct {
	ID           string         `json:"-"`
	CalendarID   string         `json:"-"`
	Title        string         `json:"-"`
	Start        time.Time      `json:"-"`
	End          time.Time      `json:"-"`
	AllDay       bool           `json:"-"`
	Rrule        string         `json:"-"`
	ParentID     *string        `json:"-"`
	RecurrenceID *time.Time     `json:"-"`
	Tzid         *string        `json:"-"`
	Reminders    []reminderWire `json:"-"`
}

// createEventRequestWire is createEventRequest's actual JSON shape: start/end
// are strings so their format can branch on AllDay (ADR-0017).
type createEventRequestWire struct {
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
}

func (r createEventRequest) MarshalJSON() ([]byte, error) {
	return json.Marshal(createEventRequestWire{
		ID:           r.ID,
		CalendarID:   r.CalendarID,
		Title:        r.Title,
		Start:        formatEventTime(r.Start, r.AllDay),
		End:          formatEventTime(r.End, r.AllDay),
		AllDay:       r.AllDay,
		Rrule:        r.Rrule,
		ParentID:     r.ParentID,
		RecurrenceID: r.RecurrenceID,
		Tzid:         r.Tzid,
		Reminders:    r.Reminders,
	})
}

func (r *createEventRequest) UnmarshalJSON(data []byte) error {
	var wire createEventRequestWire
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	start, err := parseEventTime(wire.Start, wire.AllDay)
	if err != nil {
		return fmt.Errorf("invalid start: %w", err)
	}
	end, err := parseEventTime(wire.End, wire.AllDay)
	if err != nil {
		return fmt.Errorf("invalid end: %w", err)
	}
	*r = createEventRequest{
		ID:           wire.ID,
		CalendarID:   wire.CalendarID,
		Title:        wire.Title,
		Start:        start,
		End:          end,
		AllDay:       wire.AllDay,
		Rrule:        wire.Rrule,
		ParentID:     wire.ParentID,
		RecurrenceID: wire.RecurrenceID,
		Tzid:         wire.Tzid,
		Reminders:    wire.Reminders,
	}
	return nil
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

	if _, err := uuid.Parse(req.ID); err != nil {
		httpresponse.Error(w, http.StatusBadRequest, "invalid_request", "id must be a valid UUID")
		return
	}

	event, err := h.events.Create(r.Context(), userID, req.ID, req.CalendarID, req.Title, req.Start, req.End, req.AllDay, req.Rrule, req.ParentID, req.RecurrenceID, req.Tzid, fromReminderWire(req.Reminders))
	switch {
	case errors.Is(err, service.ErrInvalidTitle):
		httpresponse.Error(w, http.StatusBadRequest, "invalid_request", "title must not be empty")
		return
	case errors.Is(err, service.ErrInvalidTimeRange):
		httpresponse.Error(w, http.StatusBadRequest, "invalid_request", "end must be after start")
		return
	case errors.Is(err, service.ErrInvalidRecurrenceRule):
		httpresponse.Error(w, http.StatusBadRequest, "invalid_request", "recurrence rule is invalid")
		return
	case errors.Is(err, service.ErrInvalidReminderChannel):
		httpresponse.Error(w, http.StatusBadRequest, "invalid_request", "reminder channel must be \"notification\" or \"email\"")
		return
	case errors.Is(err, service.ErrInvalidOverride):
		httpresponse.Error(w, http.StatusBadRequest, "invalid_request", "an override must not have its own recurrence rule, and requires a recurrence id")
		return
	case errors.Is(err, service.ErrParentIsOverride):
		httpresponse.Error(w, http.StatusBadRequest, "invalid_request", "parent event must be a master, not an override")
		return
	case errors.Is(err, service.ErrParentNotFound):
		httpresponse.Error(w, http.StatusBadRequest, "invalid_request", "parent event not found")
		return
	case errors.Is(err, service.ErrCalendarNotFound):
		httpresponse.Error(w, http.StatusBadRequest, "invalid_request", "calendar not found")
		return
	case err != nil:
		httpresponse.Error(w, http.StatusInternalServerError, "internal_error", "failed to create event")
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
	if errors.Is(err, repository.ErrNotFound) {
		httpresponse.Error(w, http.StatusNotFound, "not_found", "event not found")
		return
	}
	if err != nil {
		httpresponse.Error(w, http.StatusInternalServerError, "internal_error", "failed to load event")
		return
	}

	httpresponse.JSON(w, http.StatusOK, toEventResponse(event))
}

type updateEventRequest struct {
	CalendarID string         `json:"-"`
	Title      string         `json:"-"`
	Start      time.Time      `json:"-"`
	End        time.Time      `json:"-"`
	AllDay     bool           `json:"-"`
	Rrule      string         `json:"-"`
	Tzid       *string        `json:"-"`
	Reminders  []reminderWire `json:"-"`
}

// updateEventRequestWire is updateEventRequest's actual JSON shape: start/end
// are strings so their format can branch on AllDay (ADR-0017).
type updateEventRequestWire struct {
	CalendarID string         `json:"calendarId"`
	Title      string         `json:"title"`
	Start      string         `json:"start"`
	End        string         `json:"end"`
	AllDay     bool           `json:"allDay,omitempty"`
	Rrule      string         `json:"rrule"`
	Tzid       *string        `json:"tzid,omitempty"`
	Reminders  []reminderWire `json:"reminders,omitempty"`
}

func (r updateEventRequest) MarshalJSON() ([]byte, error) {
	return json.Marshal(updateEventRequestWire{
		CalendarID: r.CalendarID,
		Title:      r.Title,
		Start:      formatEventTime(r.Start, r.AllDay),
		End:        formatEventTime(r.End, r.AllDay),
		AllDay:     r.AllDay,
		Rrule:      r.Rrule,
		Tzid:       r.Tzid,
		Reminders:  r.Reminders,
	})
}

func (r *updateEventRequest) UnmarshalJSON(data []byte) error {
	var wire updateEventRequestWire
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	start, err := parseEventTime(wire.Start, wire.AllDay)
	if err != nil {
		return fmt.Errorf("invalid start: %w", err)
	}
	end, err := parseEventTime(wire.End, wire.AllDay)
	if err != nil {
		return fmt.Errorf("invalid end: %w", err)
	}
	*r = updateEventRequest{
		CalendarID: wire.CalendarID,
		Title:      wire.Title,
		Start:      start,
		End:        end,
		AllDay:     wire.AllDay,
		Rrule:      wire.Rrule,
		Tzid:       wire.Tzid,
		Reminders:  wire.Reminders,
	}
	return nil
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

	event, err := h.events.Update(r.Context(), userID, id, req.CalendarID, req.Title, req.Start, req.End, req.AllDay, req.Rrule, req.Tzid, fromReminderWire(req.Reminders))
	switch {
	case errors.Is(err, service.ErrInvalidTitle):
		httpresponse.Error(w, http.StatusBadRequest, "invalid_request", "title must not be empty")
		return
	case errors.Is(err, service.ErrInvalidTimeRange):
		httpresponse.Error(w, http.StatusBadRequest, "invalid_request", "end must be after start")
		return
	case errors.Is(err, service.ErrInvalidRecurrenceRule):
		httpresponse.Error(w, http.StatusBadRequest, "invalid_request", "recurrence rule is invalid")
		return
	case errors.Is(err, service.ErrInvalidReminderChannel):
		httpresponse.Error(w, http.StatusBadRequest, "invalid_request", "reminder channel must be \"notification\" or \"email\"")
		return
	case errors.Is(err, service.ErrCalendarNotFound):
		httpresponse.Error(w, http.StatusBadRequest, "invalid_request", "calendar not found")
		return
	case errors.Is(err, repository.ErrNotFound):
		httpresponse.Error(w, http.StatusNotFound, "not_found", "event not found")
		return
	case err != nil:
		httpresponse.Error(w, http.StatusInternalServerError, "internal_error", "failed to update event")
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
	if errors.Is(err, repository.ErrNotFound) {
		httpresponse.Error(w, http.StatusNotFound, "not_found", "event not found")
		return
	}
	if err != nil {
		httpresponse.Error(w, http.StatusInternalServerError, "internal_error", "failed to delete event")
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
	switch {
	case errors.Is(err, service.ErrParentNotFound):
		httpresponse.Error(w, http.StatusNotFound, "not_found", "event not found")
		return
	case errors.Is(err, service.ErrParentIsOverride):
		httpresponse.Error(w, http.StatusBadRequest, "invalid_request", "parent event must be a master, not an override")
		return
	case errors.Is(err, service.ErrParentNotRecurring):
		httpresponse.Error(w, http.StatusBadRequest, "invalid_request", "parent event does not recur")
		return
	case err != nil:
		httpresponse.Error(w, http.StatusInternalServerError, "internal_error", "failed to add exception")
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
	switch {
	case errors.Is(err, service.ErrParentNotFound):
		httpresponse.Error(w, http.StatusNotFound, "not_found", "event not found")
		return
	case err != nil:
		httpresponse.Error(w, http.StatusInternalServerError, "internal_error", "failed to reparent series")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
