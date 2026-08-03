package handlers

import (
	"encoding/json"
	"errors"
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

type eventResponse struct {
	ID         string    `json:"id"`
	CalendarID string    `json:"calendarId"`
	Title      string    `json:"title"`
	Start      time.Time `json:"start"`
	End        time.Time `json:"end"`
	Rrule      string    `json:"rrule,omitempty"`
	// ParentID and RecurrenceID are present only on an Override — a standalone
	// Event that replaces one Occurrence of its parent's series (ADR-0016).
	ParentID     *string    `json:"parentId,omitempty"`
	RecurrenceID *time.Time `json:"recurrenceId,omitempty"`
	// Exdates lists a Master's cancelled Occurrence starts (Exceptions).
	// Always absent on an Override.
	Exdates []time.Time `json:"exdates,omitempty"`
}

func toEventResponse(e repository.Event) eventResponse {
	return eventResponse{
		ID:           e.ID,
		CalendarID:   e.CalendarID,
		Title:        e.Title,
		Start:        e.Start,
		End:          e.End,
		Rrule:        e.Rrule,
		ParentID:     e.ParentID,
		RecurrenceID: e.RecurrenceID,
		Exdates:      e.Exdates,
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
	ID           string     `json:"id"`
	CalendarID   string     `json:"calendarId"`
	Title        string     `json:"title"`
	Start        time.Time  `json:"start"`
	End          time.Time  `json:"end"`
	Rrule        string     `json:"rrule"`
	ParentID     *string    `json:"parentId,omitempty"`
	RecurrenceID *time.Time `json:"recurrenceId,omitempty"`
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

	event, err := h.events.Create(r.Context(), userID, req.ID, req.CalendarID, req.Title, req.Start, req.End, req.Rrule, req.ParentID, req.RecurrenceID)
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
	CalendarID string    `json:"calendarId"`
	Title      string    `json:"title"`
	Start      time.Time `json:"start"`
	End        time.Time `json:"end"`
	Rrule      string    `json:"rrule"`
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

	event, err := h.events.Update(r.Context(), userID, id, req.CalendarID, req.Title, req.Start, req.End, req.Rrule)
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
