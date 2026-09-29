// availability_schedule.go implements AvailabilityScheduleHandler:
// List/Create/Update/Delete backing the Availability Schedules Settings
// Section (#320, ADR-0085). A Schedule is private outright and carries no
// Workspace of its own, so every route resolves by the caller's own userID
// alone and renders a mismatch as 404 — a Schedule belonging to someone else
// is indistinguishable from one that does not exist.
package handlers

import (
	"net/http"

	"github.com/XiovV/calich/server/internal/httpauth"
	"github.com/XiovV/calich/server/internal/httpresponse"
	"github.com/XiovV/calich/server/internal/repository"
	"github.com/XiovV/calich/server/internal/service"
)

type AvailabilityScheduleHandler struct {
	schedules *service.AvailabilityScheduleService
}

func NewAvailabilityScheduleHandler(schedules *service.AvailabilityScheduleService) *AvailabilityScheduleHandler {
	return &AvailabilityScheduleHandler{schedules: schedules}
}

type availabilityRangeDTO struct {
	Weekday     int `json:"weekday"`
	StartMinute int `json:"startMinute"`
	EndMinute   int `json:"endMinute"`
}

type availabilityScheduleResponse struct {
	ID     int64                  `json:"id"`
	Name   string                 `json:"name"`
	Tzid   string                 `json:"tzid"`
	Ranges []availabilityRangeDTO `json:"ranges"`
}

func toAvailabilityScheduleResponse(schedule repository.AvailabilitySchedule) availabilityScheduleResponse {
	ranges := make([]availabilityRangeDTO, len(schedule.Ranges))
	for i, rng := range schedule.Ranges {
		ranges[i] = availabilityRangeDTO{Weekday: rng.Weekday, StartMinute: rng.StartMinute, EndMinute: rng.EndMinute}
	}
	return availabilityScheduleResponse{ID: schedule.ID, Name: schedule.Name, Tzid: schedule.Tzid, Ranges: ranges}
}

func toAvailabilityRanges(dtos []availabilityRangeDTO) []repository.AvailabilityRange {
	ranges := make([]repository.AvailabilityRange, len(dtos))
	for i, dto := range dtos {
		ranges[i] = repository.AvailabilityRange{Weekday: dto.Weekday, StartMinute: dto.StartMinute, EndMinute: dto.EndMinute}
	}
	return ranges
}

// availabilityScheduleErrors renders the sentinels every
// AvailabilityScheduleService call can return: an empty name, an invalid
// timezone, an invalid range, and repository.ErrNotFound for a Schedule that
// doesn't exist or belongs to another User — the two are indistinguishable
// by design.
var availabilityScheduleErrors = []errorCase{
	{service.ErrInvalidScheduleName, badRequest(service.ErrInvalidScheduleName.Error())},
	{service.ErrInvalidTimezone, badRequest(service.ErrInvalidTimezone.Error())},
	{service.ErrInvalidAvailabilityRange, badRequest(service.ErrInvalidAvailabilityRange.Error())},
	{service.ErrScheduleReferenced, conflict("schedule_referenced", service.ErrScheduleReferenced.Error())},
	{repository.ErrNotFound, notFound("availability schedule not found")},
}

// List serves GET /api/availability-schedules: every Schedule the caller
// owns, seeding a "Default" one first if they have none yet (ADR-0085). tz,
// an optional query param carrying the caller's own browser-detected IANA
// zone, seeds that Default Schedule's timezone the one time it doesn't exist
// yet — ignored otherwise.
func (h *AvailabilityScheduleHandler) List(w http.ResponseWriter, r *http.Request) {
	userID := httpauth.MustUserID(r.Context())

	schedules, err := h.schedules.ListForUser(r.Context(), userID, r.URL.Query().Get("tz"))
	if respondError(w, err, availabilityScheduleErrors, "failed to list availability schedules") {
		return
	}

	response := make([]availabilityScheduleResponse, len(schedules))
	for i, s := range schedules {
		response[i] = toAvailabilityScheduleResponse(s)
	}

	httpresponse.JSON(w, http.StatusOK, response)
}

type createAvailabilityScheduleRequest struct {
	Name   string                 `json:"name"`
	Tzid   string                 `json:"tzid"`
	Ranges []availabilityRangeDTO `json:"ranges"`
}

// Create makes a new Schedule for the caller.
func (h *AvailabilityScheduleHandler) Create(w http.ResponseWriter, r *http.Request) {
	userID := httpauth.MustUserID(r.Context())

	req, ok := decodeJSON[createAvailabilityScheduleRequest](w, r)
	if !ok {
		return
	}

	schedule, err := h.schedules.Create(r.Context(), userID, req.Name, req.Tzid, toAvailabilityRanges(req.Ranges))
	if respondError(w, err, availabilityScheduleErrors, "failed to create availability schedule") {
		return
	}

	httpresponse.JSON(w, http.StatusCreated, toAvailabilityScheduleResponse(schedule))
}

// updateAvailabilityScheduleRequest is the same wholesale-replace shape as
// createAvailabilityScheduleRequest — Update, like Create, always takes
// name, timezone and the entire range set together, so there is no
// partial-update variant to give it a distinct shape.
type updateAvailabilityScheduleRequest = createAvailabilityScheduleRequest

// Update replaces a Schedule's name, timezone and entire weekly range set at
// once.
func (h *AvailabilityScheduleHandler) Update(w http.ResponseWriter, r *http.Request) {
	userID := httpauth.MustUserID(r.Context())
	id, ok := parseInt64Param(w, r, "id")
	if !ok {
		return
	}

	req, ok := decodeJSON[updateAvailabilityScheduleRequest](w, r)
	if !ok {
		return
	}

	schedule, err := h.schedules.Update(r.Context(), userID, id, req.Name, req.Tzid, toAvailabilityRanges(req.Ranges))
	if respondError(w, err, availabilityScheduleErrors, "failed to update availability schedule") {
		return
	}

	httpresponse.JSON(w, http.StatusOK, toAvailabilityScheduleResponse(schedule))
}

// Delete removes a Schedule outright.
func (h *AvailabilityScheduleHandler) Delete(w http.ResponseWriter, r *http.Request) {
	userID := httpauth.MustUserID(r.Context())
	id, ok := parseInt64Param(w, r, "id")
	if !ok {
		return
	}

	if err := h.schedules.Delete(r.Context(), userID, id); respondError(w, err, availabilityScheduleErrors, "failed to delete availability schedule") {
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
