// reminders.go implements EventHandler's Reminder endpoints (ADR-0064): a
// User's own Reminders on one Event, on their own read and write path
// rather than the Event create/update payload — same posture as the
// deleted /reminder-override endpoints, but for a plain personal Reminder
// list rather than a modifier over a shared one.
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

var remindersErrors = []errorCase{
	{service.ErrInvalidReminderChannel, badRequest("reminder channel must be \"notification\" or \"email\"")},
	{service.ErrCalendarNotFound, notFound("event not found")},
	{service.ErrCalendarReadOnly, forbidden("calendar is read-only")},
	{repository.ErrNotFound, notFound("event not found")},
}

// GetReminders serves GET /api/events/{id}/reminders: the caller's own
// Reminders on the Event — empty, not an error, if they've never set any.
// Any User with at least Viewer Access to the Event's Calendar may call
// this.
func (h *EventHandler) GetReminders(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpauth.UserIDFromContext(r.Context())
	if !ok {
		httpresponse.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}

	id := chi.URLParam(r, "id")

	reminders, err := h.events.GetReminders(r.Context(), userID, id)
	if respondError(w, err, remindersErrors, "failed to get reminders") {
		return
	}

	httpresponse.JSON(w, http.StatusOK, toReminderWire(reminders))
}

type setRemindersRequest struct {
	Reminders []reminderWire `json:"reminders"`
}

// SetReminders serves PUT /api/events/{id}/reminders: replaces the caller's
// own Reminders on the Event wholesale, never touching another User's rows
// on the same Event (ADR-0064). Still Editor-gated at this stage — #211
// opens this to a Viewer/Attendee with no write Access to the Event itself.
func (h *EventHandler) SetReminders(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpauth.UserIDFromContext(r.Context())
	if !ok {
		httpresponse.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}

	id := chi.URLParam(r, "id")

	var req setRemindersRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpresponse.Error(w, http.StatusBadRequest, "invalid_request", "request body must be valid JSON")
		return
	}

	reminders, err := h.events.SetReminders(r.Context(), userID, id, fromReminderWire(req.Reminders))
	if respondError(w, err, remindersErrors, "failed to set reminders") {
		return
	}

	httpresponse.JSON(w, http.StatusOK, toReminderWire(reminders))
}
