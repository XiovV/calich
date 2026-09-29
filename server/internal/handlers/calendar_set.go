// calendar_set.go implements CalendarSetHandler: Create/List/Rename/Delete
// back the Calendar Sets Settings Section (#301, ADR-0082). A Calendar Set
// is private outright, so every route resolves by the caller's own userID
// and the active workspaceID together and renders a mismatch as 404 — a Set
// belonging to someone else is indistinguishable from one that does not
// exist.
package handlers

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/XiovV/calich/server/internal/httpauth"
	"github.com/XiovV/calich/server/internal/httpresponse"
	"github.com/XiovV/calich/server/internal/repository"
	"github.com/XiovV/calich/server/internal/service"
)

type CalendarSetHandler struct {
	sets *service.CalendarSetService
}

func NewCalendarSetHandler(sets *service.CalendarSetService) *CalendarSetHandler {
	return &CalendarSetHandler{sets: sets}
}

type calendarSetResponse struct {
	ID          int64    `json:"id"`
	Name        string   `json:"name"`
	CalendarIDs []string `json:"calendarIds"`
}

// toCalendarSetResponse renders a just-created Set, which carries no
// membership of its own yet (Create takes a name only — membership is added
// separately). Create-only: a renamed Set may already have members, so
// Rename goes through toCalendarSetWithMembersResponse instead.
func toCalendarSetResponse(s repository.CalendarSet) calendarSetResponse {
	return calendarSetResponse{ID: s.ID, Name: s.Name, CalendarIDs: []string{}}
}

// toCalendarSetWithMembersResponse renders List's shape (ADR-0082): a Set
// alongside its member Calendar ids, so the switcher and the membership
// dialog both have what they need the moment they have the Set itself.
func toCalendarSetWithMembersResponse(s service.CalendarSetWithMembers) calendarSetResponse {
	ids := s.CalendarIDs
	if ids == nil {
		ids = []string{}
	}
	return calendarSetResponse{ID: s.ID, Name: s.Name, CalendarIDs: ids}
}

// calendarSetErrors renders the sentinels every CalendarSetService call can
// return: an empty name, and repository.ErrNotFound for a Set that doesn't
// exist, belongs to another User, or belongs to another Workspace — the
// three are indistinguishable by design.
var calendarSetErrors = []errorCase{
	{service.ErrInvalidCalendarSetName, badRequest(service.ErrInvalidCalendarSetName.Error())},
	{repository.ErrNotFound, notFound("calendar set not found")},
}

// List serves GET /api/calendar-sets: every Calendar Set the caller owns in
// their active Workspace.
func (h *CalendarSetHandler) List(w http.ResponseWriter, r *http.Request) {
	userID := httpauth.MustUserID(r.Context())
	workspaceID := httpauth.MustWorkspaceID(r.Context())

	sets, err := h.sets.ListForUser(r.Context(), userID, workspaceID)
	if respondError(w, err, calendarSetErrors, "failed to list calendar sets") {
		return
	}

	response := make([]calendarSetResponse, len(sets))
	for i, s := range sets {
		response[i] = toCalendarSetWithMembersResponse(s)
	}

	httpresponse.JSON(w, http.StatusOK, response)
}

type createCalendarSetRequest struct {
	Name string `json:"name"`
}

// Create makes a new Calendar Set in the caller's active Workspace, named
// only — membership is added separately.
func (h *CalendarSetHandler) Create(w http.ResponseWriter, r *http.Request) {
	userID := httpauth.MustUserID(r.Context())
	workspaceID := httpauth.MustWorkspaceID(r.Context())

	req, ok := decodeJSON[createCalendarSetRequest](w, r)
	if !ok {
		return
	}

	set, err := h.sets.Create(r.Context(), userID, workspaceID, req.Name)
	if respondError(w, err, calendarSetErrors, "failed to create calendar set") {
		return
	}

	httpresponse.JSON(w, http.StatusCreated, toCalendarSetResponse(set))
}

type renameCalendarSetRequest struct {
	Name string `json:"name"`
}

// Rename changes a Calendar Set's name.
func (h *CalendarSetHandler) Rename(w http.ResponseWriter, r *http.Request) {
	userID := httpauth.MustUserID(r.Context())
	workspaceID := httpauth.MustWorkspaceID(r.Context())
	id, ok := parseInt64Param(w, r, "id")
	if !ok {
		return
	}

	req, ok := decodeJSON[renameCalendarSetRequest](w, r)
	if !ok {
		return
	}

	set, err := h.sets.Rename(r.Context(), userID, workspaceID, id, req.Name)
	if respondError(w, err, calendarSetErrors, "failed to rename calendar set") {
		return
	}

	httpresponse.JSON(w, http.StatusOK, toCalendarSetWithMembersResponse(set))
}

// Delete removes a Calendar Set outright. Destroys no Calendar and no Event.
func (h *CalendarSetHandler) Delete(w http.ResponseWriter, r *http.Request) {
	userID := httpauth.MustUserID(r.Context())
	workspaceID := httpauth.MustWorkspaceID(r.Context())
	id, ok := parseInt64Param(w, r, "id")
	if !ok {
		return
	}

	if err := h.sets.Delete(r.Context(), userID, workspaceID, id); respondError(w, err, calendarSetErrors, "failed to delete calendar set") {
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// AddCalendar serves PUT /api/calendar-sets/{id}/calendars/{calendarId}:
// puts a Calendar into a Set's membership (#302, ADR-0082). PUT, not POST —
// re-adding a Calendar already in the Set answers 204 same as a fresh add.
// Refuses (as calendarSetErrors' repository.ErrNotFound case) a Calendar
// from another Workspace or one the caller has no Access to, exactly as it
// refuses a Set that isn't the caller's own.
func (h *CalendarSetHandler) AddCalendar(w http.ResponseWriter, r *http.Request) {
	userID := httpauth.MustUserID(r.Context())
	workspaceID := httpauth.MustWorkspaceID(r.Context())
	id, ok := parseInt64Param(w, r, "id")
	if !ok {
		return
	}
	calendarID := chi.URLParam(r, "calendarId")

	if err := h.sets.AddCalendar(r.Context(), userID, workspaceID, id, calendarID); respondError(w, err, calendarSetErrors, "failed to add calendar to calendar set") {
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// RemoveCalendar serves DELETE /api/calendar-sets/{id}/calendars/{calendarId}:
// takes a Calendar out of a Set's membership (#302, ADR-0082). The reverse
// cascade — a deleted, unsubscribed, or unshared Calendar leaving every Set
// it was in — is the calendars table's own ON DELETE CASCADE on
// calendar_set_members, not this handler; a Set emptied that way survives,
// same as one emptied through this route.
func (h *CalendarSetHandler) RemoveCalendar(w http.ResponseWriter, r *http.Request) {
	userID := httpauth.MustUserID(r.Context())
	workspaceID := httpauth.MustWorkspaceID(r.Context())
	id, ok := parseInt64Param(w, r, "id")
	if !ok {
		return
	}
	calendarID := chi.URLParam(r, "calendarId")

	if err := h.sets.RemoveCalendar(r.Context(), userID, workspaceID, id, calendarID); respondError(w, err, calendarSetErrors, "failed to remove calendar from calendar set") {
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
