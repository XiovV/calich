// booking_link.go implements BookingLinkHandler: List/Create/Update/Delete/
// Duplicate back the Booking links sidebar section (#322, ADR-0084,
// ADR-0087), plus AddConflictCalendar/RemoveConflictCalendar for its
// Conflict set. Scoped like Task Lists — every route resolves by (id,
// caller, active workspace) and renders a mismatch as 404, a Booking Link
// belonging to someone else being indistinguishable from one that does not
// exist.
package handlers

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/XiovV/calich/server/internal/httpauth"
	"github.com/XiovV/calich/server/internal/httpresponse"
	"github.com/XiovV/calich/server/internal/repository"
	"github.com/XiovV/calich/server/internal/service"
)

type BookingLinkHandler struct {
	links *service.BookingLinkService
}

func NewBookingLinkHandler(links *service.BookingLinkService) *BookingLinkHandler {
	return &BookingLinkHandler{links: links}
}

type bookingLinkResponse struct {
	ID                     int64    `json:"id"`
	Title                  string   `json:"title"`
	Slug                   string   `json:"slug"`
	DurationMinutes        int      `json:"durationMinutes"`
	Visibility             string   `json:"visibility"`
	AvailabilityScheduleID int64    `json:"availabilityScheduleId"`
	BookIntoCalendarID     string   `json:"bookIntoCalendarId"`
	Location               string   `json:"location"`
	Description            string   `json:"description"`
	MinimumNoticeMinutes   int      `json:"minimumNoticeMinutes"`
	BookingHorizonDays     int      `json:"bookingHorizonDays"`
	TasksInConflictSet     bool     `json:"tasksInConflictSet"`
	ConflictCalendarIDs    []string `json:"conflictCalendarIds"`
}

func toBookingLinkResponse(l repository.BookingLink) bookingLinkResponse {
	ids := l.ConflictCalendarIDs
	if ids == nil {
		ids = []string{}
	}
	return bookingLinkResponse{
		ID:                     l.ID,
		Title:                  l.Title,
		Slug:                   l.Slug,
		DurationMinutes:        l.DurationMinutes,
		Visibility:             l.Visibility,
		AvailabilityScheduleID: l.AvailabilityScheduleID,
		BookIntoCalendarID:     l.BookIntoCalendarID,
		Location:               l.Location,
		Description:            l.Description,
		MinimumNoticeMinutes:   l.MinimumNoticeMinutes,
		BookingHorizonDays:     l.BookingHorizonDays,
		TasksInConflictSet:     l.TasksInConflictSet,
		ConflictCalendarIDs:    ids,
	}
}

// bookingLinkWriteRequest is Create and Update's shared body shape — Update,
// like Create, always takes every field together (BookingLinkService.Update
// replaces them wholesale), so there is no partial-update variant to give it
// a distinct shape.
type bookingLinkWriteRequest struct {
	Title                  string `json:"title"`
	Slug                   string `json:"slug"`
	DurationMinutes        int    `json:"durationMinutes"`
	Visibility             string `json:"visibility"`
	AvailabilityScheduleID int64  `json:"availabilityScheduleId"`
	BookIntoCalendarID     string `json:"bookIntoCalendarId"`
	Location               string `json:"location"`
	Description            string `json:"description"`
	MinimumNoticeMinutes   int    `json:"minimumNoticeMinutes"`
	BookingHorizonDays     int    `json:"bookingHorizonDays"`
	TasksInConflictSet     bool   `json:"tasksInConflictSet"`
}

func (req bookingLinkWriteRequest) toWrite() service.BookingLinkWrite {
	return service.BookingLinkWrite{
		Title:                  req.Title,
		Slug:                   req.Slug,
		DurationMinutes:        req.DurationMinutes,
		Visibility:             req.Visibility,
		AvailabilityScheduleID: req.AvailabilityScheduleID,
		BookIntoCalendarID:     req.BookIntoCalendarID,
		Location:               req.Location,
		Description:            req.Description,
		MinimumNoticeMinutes:   req.MinimumNoticeMinutes,
		BookingHorizonDays:     req.BookingHorizonDays,
		TasksInConflictSet:     req.TasksInConflictSet,
	}
}

// bookingLinkErrors renders the sentinels every BookingLinkService call can
// return, plus repository.ErrNotFound for a Booking Link that doesn't exist,
// belongs to another User, or belongs to another Workspace — the three are
// indistinguishable by design.
var bookingLinkErrors = []errorCase{
	{service.ErrInvalidBookingLinkTitle, badRequest(service.ErrInvalidBookingLinkTitle.Error())},
	{service.ErrInvalidSlug, badRequest(service.ErrInvalidSlug.Error())},
	{service.ErrInvalidDuration, badRequest(service.ErrInvalidDuration.Error())},
	{service.ErrInvalidVisibility, badRequest(service.ErrInvalidVisibility.Error())},
	{service.ErrInvalidMinimumNotice, badRequest(service.ErrInvalidMinimumNotice.Error())},
	{service.ErrInvalidBookingHorizon, badRequest(service.ErrInvalidBookingHorizon.Error())},
	{service.ErrScheduleNotFound, badRequest(service.ErrScheduleNotFound.Error())},
	{service.ErrBookIntoCalendarNotFound, badRequest(service.ErrBookIntoCalendarNotFound.Error())},
	{service.ErrCannotRemoveBookIntoCalendar, badRequest(service.ErrCannotRemoveBookIntoCalendar.Error())},
	{service.ErrSlugTaken, conflict("slug_taken", "slug is already taken")},
	{service.ErrInvalidMonth, badRequest(service.ErrInvalidMonth.Error())},
	{repository.ErrNotFound, notFound("booking link not found")},
}

// List serves GET /api/booking-links: every Booking Link the caller owns in
// their active Workspace.
func (h *BookingLinkHandler) List(w http.ResponseWriter, r *http.Request) {
	userID := httpauth.MustUserID(r.Context())
	workspaceID := httpauth.MustWorkspaceID(r.Context())

	links, err := h.links.ListForUser(r.Context(), userID, workspaceID)
	if respondError(w, err, bookingLinkErrors, "failed to list booking links") {
		return
	}

	response := make([]bookingLinkResponse, len(links))
	for i, l := range links {
		response[i] = toBookingLinkResponse(l)
	}

	httpresponse.JSON(w, http.StatusOK, response)
}

// Create makes a new Booking Link in the caller's active Workspace. Its
// Conflict set is seeded automatically; claims a Handle for the caller if
// this is their first Booking Link and they have none yet.
func (h *BookingLinkHandler) Create(w http.ResponseWriter, r *http.Request) {
	userID := httpauth.MustUserID(r.Context())
	workspaceID := httpauth.MustWorkspaceID(r.Context())

	req, ok := decodeJSON[bookingLinkWriteRequest](w, r)
	if !ok {
		return
	}

	link, err := h.links.Create(r.Context(), userID, workspaceID, req.toWrite())
	if respondError(w, err, bookingLinkErrors, "failed to create booking link") {
		return
	}

	httpresponse.JSON(w, http.StatusCreated, toBookingLinkResponse(link))
}

// Update replaces a Booking Link's fields wholesale.
func (h *BookingLinkHandler) Update(w http.ResponseWriter, r *http.Request) {
	userID := httpauth.MustUserID(r.Context())
	workspaceID := httpauth.MustWorkspaceID(r.Context())
	id, ok := parseInt64Param(w, r, "id")
	if !ok {
		return
	}

	req, ok := decodeJSON[bookingLinkWriteRequest](w, r)
	if !ok {
		return
	}

	link, err := h.links.Update(r.Context(), userID, workspaceID, id, req.toWrite())
	if respondError(w, err, bookingLinkErrors, "failed to update booking link") {
		return
	}

	httpresponse.JSON(w, http.StatusOK, toBookingLinkResponse(link))
}

// Delete removes a Booking Link outright.
func (h *BookingLinkHandler) Delete(w http.ResponseWriter, r *http.Request) {
	userID := httpauth.MustUserID(r.Context())
	workspaceID := httpauth.MustWorkspaceID(r.Context())
	id, ok := parseInt64Param(w, r, "id")
	if !ok {
		return
	}

	if err := h.links.Delete(r.Context(), userID, workspaceID, id); respondError(w, err, bookingLinkErrors, "failed to delete booking link") {
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// Duplicate clones a Booking Link, deriving a fresh Slug and Title and
// copying its Conflict set verbatim.
func (h *BookingLinkHandler) Duplicate(w http.ResponseWriter, r *http.Request) {
	userID := httpauth.MustUserID(r.Context())
	workspaceID := httpauth.MustWorkspaceID(r.Context())
	id, ok := parseInt64Param(w, r, "id")
	if !ok {
		return
	}

	link, err := h.links.Duplicate(r.Context(), userID, workspaceID, id)
	if respondError(w, err, bookingLinkErrors, "failed to duplicate booking link") {
		return
	}

	httpresponse.JSON(w, http.StatusCreated, toBookingLinkResponse(link))
}

// AddConflictCalendar serves PUT /api/booking-links/{id}/conflict-set/calendars/{calendarId}:
// puts a Calendar into a Booking Link's Conflict set. PUT, not POST —
// re-adding a Calendar already in the set answers 204 same as a fresh add.
func (h *BookingLinkHandler) AddConflictCalendar(w http.ResponseWriter, r *http.Request) {
	userID := httpauth.MustUserID(r.Context())
	workspaceID := httpauth.MustWorkspaceID(r.Context())
	id, ok := parseInt64Param(w, r, "id")
	if !ok {
		return
	}
	calendarID := chi.URLParam(r, "calendarId")

	if err := h.links.AddConflictCalendar(r.Context(), userID, workspaceID, id, calendarID); respondError(w, err, bookingLinkErrors, "failed to add conflict calendar") {
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// RemoveConflictCalendar serves DELETE /api/booking-links/{id}/conflict-set/calendars/{calendarId}:
// takes a Calendar out of a Booking Link's Conflict set. Refuses the
// Book-into Calendar — permanently in the set and unremovable (ADR-0087).
func (h *BookingLinkHandler) RemoveConflictCalendar(w http.ResponseWriter, r *http.Request) {
	userID := httpauth.MustUserID(r.Context())
	workspaceID := httpauth.MustWorkspaceID(r.Context())
	id, ok := parseInt64Param(w, r, "id")
	if !ok {
		return
	}
	calendarID := chi.URLParam(r, "calendarId")

	if err := h.links.RemoveConflictCalendar(r.Context(), userID, workspaceID, id, calendarID); respondError(w, err, bookingLinkErrors, "failed to remove conflict calendar") {
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

type slotsResponse struct {
	Slots []time.Time `json:"slots"`
}

// Slots serves GET /api/booking-links/{id}/slots?year=&month=: every
// bookable slot start for id within the given calendar month (#323),
// derived server-side so the eventual public page (#324) — which has no
// Session — can be told the same answer this route computes. year and
// month are required query params; month is 1-12.
func (h *BookingLinkHandler) Slots(w http.ResponseWriter, r *http.Request) {
	userID := httpauth.MustUserID(r.Context())
	workspaceID := httpauth.MustWorkspaceID(r.Context())
	id, ok := parseInt64Param(w, r, "id")
	if !ok {
		return
	}

	year, ok := parseRequiredIntQuery(w, r, "year")
	if !ok {
		return
	}
	month, ok := parseRequiredIntQuery(w, r, "month")
	if !ok {
		return
	}

	slots, err := h.links.DeriveSlotsForMonth(r.Context(), userID, workspaceID, id, year, time.Month(month), time.Now())
	if respondError(w, err, bookingLinkErrors, "failed to derive slots") {
		return
	}

	httpresponse.JSON(w, http.StatusOK, slotsResponse{Slots: slots})
}

func parseRequiredIntQuery(w http.ResponseWriter, r *http.Request, name string) (int, bool) {
	raw := r.URL.Query().Get(name)
	value, err := strconv.Atoi(raw)
	if err != nil {
		httpresponse.Error(w, http.StatusBadRequest, "invalid_request", name+" must be an integer")
		return 0, false
	}
	return value, true
}
