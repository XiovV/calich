package handlers

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/XiovV/calendar/server/internal/httpauth"
	"github.com/XiovV/calendar/server/internal/httpresponse"
	"github.com/XiovV/calendar/server/internal/icalendar"
	"github.com/XiovV/calendar/server/internal/repository"
	"github.com/XiovV/calendar/server/internal/service"
)

type CalendarHandler struct {
	calendars *service.CalendarService
	events    *service.EventService
	imports   *service.ImportService
}

func NewCalendarHandler(calendars *service.CalendarService, events *service.EventService, imports *service.ImportService) *CalendarHandler {
	return &CalendarHandler{calendars: calendars, events: events, imports: imports}
}

type calendarResponse struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
}

func toCalendarResponse(c repository.Calendar) calendarResponse {
	return calendarResponse{ID: c.ID, Name: c.Name, Color: c.Color}
}

// calendarWriteErrors is the rendering shared by the create and update paths,
// which validate the same two fields.
var calendarWriteErrors = []errorCase{
	{service.ErrInvalidName, badRequest("name must not be empty")},
	{service.ErrInvalidColor, badRequest("color must be a valid hex color (#RGB, #RRGGBB, or #RRGGBBAA)")},
}

var calendarNotFoundErrors = []errorCase{
	{repository.ErrNotFound, notFound("calendar not found")},
}

var updateCalendarErrors = alsoHandling(calendarWriteErrors, calendarNotFoundErrors...)

func (h *CalendarHandler) List(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpauth.UserIDFromContext(r.Context())
	if !ok {
		httpresponse.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}

	calendars, err := h.calendars.List(r.Context(), userID)
	if err != nil {
		httpresponse.Error(w, http.StatusInternalServerError, "internal_error", "failed to list calendars")
		return
	}

	response := make([]calendarResponse, len(calendars))
	for i, c := range calendars {
		response[i] = toCalendarResponse(c)
	}

	httpresponse.JSON(w, http.StatusOK, response)
}

type createCalendarRequest struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
}

func (h *CalendarHandler) Create(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpauth.UserIDFromContext(r.Context())
	if !ok {
		httpresponse.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}

	var req createCalendarRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpresponse.Error(w, http.StatusBadRequest, "invalid_request", "request body must be valid JSON")
		return
	}

	if _, err := uuid.Parse(req.ID); err != nil {
		httpresponse.Error(w, http.StatusBadRequest, "invalid_request", "id must be a valid UUID")
		return
	}

	calendar, err := h.calendars.Create(r.Context(), userID, req.ID, req.Name, req.Color)
	if respondError(w, err, calendarWriteErrors, "failed to create calendar") {
		return
	}

	httpresponse.JSON(w, http.StatusCreated, toCalendarResponse(calendar))
}

func (h *CalendarHandler) Get(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpauth.UserIDFromContext(r.Context())
	if !ok {
		httpresponse.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}

	id := chi.URLParam(r, "id")

	calendar, err := h.calendars.Get(r.Context(), userID, id)
	if respondError(w, err, calendarNotFoundErrors, "failed to load calendar") {
		return
	}

	httpresponse.JSON(w, http.StatusOK, toCalendarResponse(calendar))
}

type updateCalendarRequest struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}

func (h *CalendarHandler) Update(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpauth.UserIDFromContext(r.Context())
	if !ok {
		httpresponse.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}

	id := chi.URLParam(r, "id")

	var req updateCalendarRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpresponse.Error(w, http.StatusBadRequest, "invalid_request", "request body must be valid JSON")
		return
	}

	calendar, err := h.calendars.Update(r.Context(), userID, id, req.Name, req.Color)
	if respondError(w, err, updateCalendarErrors, "failed to update calendar") {
		return
	}

	httpresponse.JSON(w, http.StatusOK, toCalendarResponse(calendar))
}

func (h *CalendarHandler) Delete(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpauth.UserIDFromContext(r.Context())
	if !ok {
		httpresponse.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}

	id := chi.URLParam(r, "id")

	err := h.calendars.Delete(r.Context(), userID, id)
	if respondError(w, err, calendarNotFoundErrors, "failed to delete calendar") {
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// icsForCalendar builds one Calendar's whole export VCALENDAR: every series
// it holds, unbounded window (#76).
func (h *CalendarHandler) icsForCalendar(ctx context.Context, userID int64, calendar repository.Calendar) ([]byte, error) {
	masters, overridesByParent, err := h.events.ListSeriesByCalendar(ctx, userID, calendar.ID)
	if err != nil {
		return nil, err
	}

	cal, err := icalendar.CalendarToICal(calendar.Name, calendar.Color, masters, overridesByParent)
	if err != nil {
		return nil, err
	}
	return icalendar.Encode(cal)
}

// ICS serves GET /api/calendars/{id}/ics: every series in the Calendar,
// unbounded window, as one VCALENDAR carrying X-WR-CALNAME/
// X-APPLE-CALENDAR-COLOR so the name and color survive export (#76).
func (h *CalendarHandler) ICS(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpauth.UserIDFromContext(r.Context())
	if !ok {
		httpresponse.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}

	id := chi.URLParam(r, "id")

	calendar, err := h.calendars.Get(r.Context(), userID, id)
	if respondError(w, err, calendarNotFoundErrors, "failed to load calendar") {
		return
	}

	body, err := h.icsForCalendar(r.Context(), userID, calendar)
	if err != nil {
		httpresponse.Error(w, http.StatusInternalServerError, "internal_error", "failed to build calendar object")
		return
	}

	httpresponse.ICS(w, sanitizeICSFilename(calendar.Name, "calendar")+".ics", body)
}

// ICSAll serves GET /api/calendars/ics: every Calendar the caller owns,
// zipped, one .ics entry per Calendar named after it (#76).
func (h *CalendarHandler) ICSAll(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpauth.UserIDFromContext(r.Context())
	if !ok {
		httpresponse.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}

	calendars, err := h.calendars.List(r.Context(), userID)
	if err != nil {
		httpresponse.Error(w, http.StatusInternalServerError, "internal_error", "failed to list calendars")
		return
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	usedNames := make(map[string]int, len(calendars))
	for _, calendar := range calendars {
		body, err := h.icsForCalendar(r.Context(), userID, calendar)
		if err != nil {
			httpresponse.Error(w, http.StatusInternalServerError, "internal_error", "failed to build calendar object")
			return
		}

		entryName := uniqueZipEntryName(usedNames, sanitizeICSFilename(calendar.Name, "calendar")+".ics")
		entry, err := zw.Create(entryName)
		if err != nil {
			httpresponse.Error(w, http.StatusInternalServerError, "internal_error", "failed to build archive")
			return
		}
		if _, err := entry.Write(body); err != nil {
			httpresponse.Error(w, http.StatusInternalServerError, "internal_error", "failed to build archive")
			return
		}
	}
	if err := zw.Close(); err != nil {
		httpresponse.Error(w, http.StatusInternalServerError, "internal_error", "failed to build archive")
		return
	}

	httpresponse.Zip(w, "calendars.zip", buf.Bytes())
}
