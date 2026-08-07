// reminder_override.go implements EventHandler's Reminder override
// endpoints (#105, ADR-0036): a User retuning, muting, or clearing their own
// personal Reminder delivery preference on an Event they have at least
// Viewer Access to. Unlike every other Event write, this never touches the
// Event itself, so it is guarded by EventService's own Access check rather
// than the CanWrite guard the rest of this file's handlers rely on.
package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/XiovV/calendar/server/internal/httpauth"
	"github.com/XiovV/calendar/server/internal/httpresponse"
	"github.com/XiovV/calendar/server/internal/repository"
	"github.com/XiovV/calendar/server/internal/service"
)

// reminderOverrideResponse is a Reminder override's wire shape. OffsetMinutes
// and Channel are independently nil when that dimension isn't overridden —
// the Event's own Reminders apply for it.
type reminderOverrideResponse struct {
	OffsetMinutes *int    `json:"offsetMinutes,omitempty"`
	Channel       *string `json:"channel,omitempty"`
	Muted         bool    `json:"muted,omitempty"`
}

func toReminderOverrideResponse(o repository.ReminderOverride) reminderOverrideResponse {
	return reminderOverrideResponse{OffsetMinutes: o.OffsetMinutes, Channel: o.Channel, Muted: o.Muted}
}

var reminderOverrideErrors = []errorCase{
	{service.ErrInvalidReminderChannel, badRequest("reminder channel must be \"notification\" or \"email\"")},
	{repository.ErrNotFound, notFound("event not found")},
}

type reminderOverrideRequest struct {
	OffsetMinutes *int    `json:"offsetMinutes,omitempty"`
	Channel       *string `json:"channel,omitempty"`
	Muted         bool    `json:"muted,omitempty"`
}

// GetReminderOverride serves GET /api/events/{id}/reminder-override: the
// caller's own current override, or a zero-value response (every field
// absent, muted false) if they've never set one — the Event's own Reminders
// apply unchanged in that case. Any User with at least Viewer Access to the
// Event's Calendar may call this.
func (h *EventHandler) GetReminderOverride(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpauth.UserIDFromContext(r.Context())
	if !ok {
		httpresponse.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}

	id := chi.URLParam(r, "id")

	override, _, err := h.events.GetReminderOverride(r.Context(), userID, id)
	if respondError(w, err, reminderOverrideErrors, "failed to get reminder override") {
		return
	}

	httpresponse.JSON(w, http.StatusOK, toReminderOverrideResponse(override))
}

// SetReminderOverride serves PUT /api/events/{id}/reminder-override: sets
// the caller's own Reminder override on the Event, replacing any override
// they already had. Any User with at least Viewer Access to the Event's
// Calendar may call this.
func (h *EventHandler) SetReminderOverride(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpauth.UserIDFromContext(r.Context())
	if !ok {
		httpresponse.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}

	id := chi.URLParam(r, "id")

	var req reminderOverrideRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpresponse.Error(w, http.StatusBadRequest, "invalid_request", "request body must be valid JSON")
		return
	}

	override, err := h.events.SetReminderOverride(r.Context(), userID, id, service.ReminderOverrideWrite{
		OffsetMinutes: req.OffsetMinutes,
		Channel:       req.Channel,
		Muted:         req.Muted,
	})
	if respondError(w, err, reminderOverrideErrors, "failed to set reminder override") {
		return
	}

	httpresponse.JSON(w, http.StatusOK, toReminderOverrideResponse(override))
}

// ClearReminderOverride serves DELETE /api/events/{id}/reminder-override:
// removes the caller's own Reminder override, falling back to the Event's
// own Reminders. A no-op if the caller never set one.
func (h *EventHandler) ClearReminderOverride(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpauth.UserIDFromContext(r.Context())
	if !ok {
		httpresponse.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}

	id := chi.URLParam(r, "id")

	if err := h.events.ClearReminderOverride(r.Context(), userID, id); respondError(w, err, reminderOverrideErrors, "failed to clear reminder override") {
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
