package handlers

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/XiovV/calendar/server/internal/httpauth"
	"github.com/XiovV/calendar/server/internal/httpresponse"
	"github.com/XiovV/calendar/server/internal/repository"
	"github.com/XiovV/calendar/server/internal/service"
)

type CalendarHandler struct {
	calendars *service.CalendarService
}

func NewCalendarHandler(calendars *service.CalendarService) *CalendarHandler {
	return &CalendarHandler{calendars: calendars}
}

type calendarResponse struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
}

func toCalendarResponse(c repository.Calendar) calendarResponse {
	return calendarResponse{ID: c.ID, Name: c.Name, Color: c.Color}
}

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
	switch {
	case errors.Is(err, service.ErrInvalidName):
		httpresponse.Error(w, http.StatusBadRequest, "invalid_request", "name must not be empty")
		return
	case errors.Is(err, service.ErrInvalidColor):
		httpresponse.Error(w, http.StatusBadRequest, "invalid_request", "color must be a valid hex color (#RGB, #RRGGBB, or #RRGGBBAA)")
		return
	case err != nil:
		httpresponse.Error(w, http.StatusInternalServerError, "internal_error", "failed to create calendar")
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
	if errors.Is(err, repository.ErrNotFound) {
		httpresponse.Error(w, http.StatusNotFound, "not_found", "calendar not found")
		return
	}
	if err != nil {
		httpresponse.Error(w, http.StatusInternalServerError, "internal_error", "failed to load calendar")
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
	switch {
	case errors.Is(err, service.ErrInvalidName):
		httpresponse.Error(w, http.StatusBadRequest, "invalid_request", "name must not be empty")
		return
	case errors.Is(err, service.ErrInvalidColor):
		httpresponse.Error(w, http.StatusBadRequest, "invalid_request", "color must be a valid hex color (#RGB, #RRGGBB, or #RRGGBBAA)")
		return
	case errors.Is(err, repository.ErrNotFound):
		httpresponse.Error(w, http.StatusNotFound, "not_found", "calendar not found")
		return
	case err != nil:
		httpresponse.Error(w, http.StatusInternalServerError, "internal_error", "failed to update calendar")
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
	if errors.Is(err, repository.ErrNotFound) {
		httpresponse.Error(w, http.StatusNotFound, "not_found", "calendar not found")
		return
	}
	if err != nil {
		httpresponse.Error(w, http.StatusInternalServerError, "internal_error", "failed to delete calendar")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
